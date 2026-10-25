package agent

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// SHUTDOWN-DPKG-001 AD-006.3 (security condition C-4): outside execReboot and
// execShutdown, no non-test code under internal/agent/... or cmd/riot-agent
// launches a power tool, names a power verb, or calls a power system call. The
// scan is AST-only: no go/types and no new dependency. What it cannot see is
// listed in the ADD as residual R-11.

const modulePath = "github.com/DesyncTheThird/rIOt"

// powerPathFile is one source file to scan. Path is relative to the package
// directory of the test, so "commands.go" is the agent package itself and
// "collectors/x.go" a subpackage.
type powerPathFile struct {
	Path string
	Src  []byte
}

type powerFileInfo struct {
	path    string
	dir     string
	file    *ast.File
	imports map[string]string // alias -> import path
}

// powerScanner folds string constants across the files of a package directory
// and across packages of this module, then applies the C-4 rules.
type powerScanner struct {
	t          *testing.T
	fset       *token.FileSet
	infos      []*powerFileInfo
	byDir      map[string][]*powerFileInfo
	consts     map[string]map[string]string // package key -> const name -> folded value
	moduleRoot string
}

var (
	powerStringRE = regexp.MustCompile(`(?i)(^|[^a-z0-9])(power-?off|halt)([^a-z0-9]|$)`)
	runCommandRE  = regexp.MustCompile(`(?i)^run\w*command$`)
	powerTools    = map[string]bool{"shutdown": true, "halt": true, "poweroff": true, "reboot": true, "init": true, "telinit": true, "kexec": true}
	powerVerbs    = map[string]bool{
		"poweroff": true, "halt": true, "reboot": true, "kexec": true,
		"poweroff.target": true, "halt.target": true, "reboot.target": true, "kexec.target": true,
	}
	powerShells = map[string]bool{"sh": true, "bash": true, "dash": true, "cmd": true, "powershell": true, "pwsh": true}
	// powerExecFuncNames are the only functions allowed to launch a power action.
	powerExemptFuncs = map[string]bool{"execReboot": true, "execShutdown": true}
)

func newPowerScanner(t *testing.T, files []powerPathFile) *powerScanner {
	t.Helper()
	s := &powerScanner{
		t: t, fset: token.NewFileSet(), byDir: map[string][]*powerFileInfo{},
		consts: map[string]map[string]string{}, moduleRoot: filepath.Join("..", ".."),
	}
	for _, f := range files {
		s.add(f.Path, f.Src)
	}
	return s
}

func (s *powerScanner) parse(filename string, src []byte) *powerFileInfo {
	file, err := parser.ParseFile(s.fset, filename, src, 0)
	require.NoError(s.t, err, "parse %s", filename)
	info := &powerFileInfo{path: filename, dir: filepath.ToSlash(filepath.Dir(filename)), file: file, imports: map[string]string{}}
	for _, im := range file.Imports {
		p, _ := strconv.Unquote(im.Path.Value)
		alias := path.Base(p)
		if im.Name != nil {
			alias = im.Name.Name
		}
		info.imports[alias] = p
	}
	return info
}

func (s *powerScanner) add(filename string, src []byte) {
	info := s.parse(filename, src)
	s.infos = append(s.infos, info)
	s.byDir[info.dir] = append(s.byDir[info.dir], info)
}

// constsOf returns the folded string constants of a package: a scanned directory
// (key = the directory) or a package of this module (key = its import path, read
// from disk on demand).
func (s *powerScanner) constsOf(key string) map[string]string {
	if c, ok := s.consts[key]; ok {
		return c
	}
	c := map[string]string{}
	s.consts[key] = c // set first, so an import cycle cannot recurse forever
	infos := s.byDir[key]
	if infos == nil && strings.HasPrefix(key, modulePath+"/") {
		dir := filepath.Join(s.moduleRoot, filepath.FromSlash(strings.TrimPrefix(key, modulePath+"/")))
		entries, err := os.ReadDir(dir)
		if err != nil {
			return c
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
				continue
			}
			src, err := os.ReadFile(filepath.Join(dir, e.Name()))
			require.NoError(s.t, err)
			infos = append(infos, s.parse(filepath.Join(dir, e.Name()), src))
		}
	}
	type spec struct {
		name string
		expr ast.Expr
		info *powerFileInfo
	}
	var specs []spec
	for _, info := range infos {
		for _, decl := range info.file.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, sp := range gd.Specs {
				vs := sp.(*ast.ValueSpec)
				if len(vs.Names) != len(vs.Values) {
					continue
				}
				for i, n := range vs.Names {
					specs = append(specs, spec{n.Name, vs.Values[i], info})
				}
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for _, sp := range specs {
			if _, done := c[sp.name]; done {
				continue
			}
			if v, ok := s.fold(sp.expr, sp.info, key); ok {
				c[sp.name] = v
				changed = true
			}
		}
	}
	return c
}

// fold evaluates a string expression made of literals, "+", parentheses and
// package-level string constants of this package or of a package of this module.
// Anything else is unfoldable (residual R-11).
func (s *powerScanner) fold(e ast.Expr, info *powerFileInfo, key string) (string, bool) {
	switch x := e.(type) {
	case *ast.BasicLit:
		if x.Kind != token.STRING {
			return "", false
		}
		v, err := strconv.Unquote(x.Value)
		return v, err == nil
	case *ast.ParenExpr:
		return s.fold(x.X, info, key)
	case *ast.BinaryExpr:
		if x.Op != token.ADD {
			return "", false
		}
		l, lok := s.fold(x.X, info, key)
		r, rok := s.fold(x.Y, info, key)
		return l + r, lok && rok
	case *ast.Ident:
		v, ok := s.consts[key][x.Name]
		return v, ok
	case *ast.SelectorExpr:
		pkg, ok := x.X.(*ast.Ident)
		if !ok {
			return "", false
		}
		imp := info.imports[pkg.Name]
		if !strings.HasPrefix(imp, modulePath+"/") {
			return "", false
		}
		v, ok := s.constsOf(imp)[x.Sel.Name]
		return v, ok
	}
	return "", false
}

// flattenArgs expands slice literals (os.StartProcess takes its argv as one).
func flattenArgs(args []ast.Expr) []ast.Expr {
	var out []ast.Expr
	for _, a := range args {
		if cl, ok := a.(*ast.CompositeLit); ok {
			out = append(out, flattenArgs(cl.Elts)...)
			continue
		}
		out = append(out, a)
	}
	return out
}

func baseTool(program string) string {
	p := strings.ToLower(strings.ReplaceAll(program, `\`, "/"))
	return strings.TrimSuffix(path.Base(p), ".exe")
}

// powerLaunchProblem reports why a launch of program with args is a power action,
// or "".
func powerLaunchProblem(program string, args []string) string {
	tool := baseTool(program)
	switch {
	case powerTools[tool]:
		return "launches the power tool " + tool
	case tool == "systemctl":
		for _, a := range args {
			if powerVerbs[strings.ToLower(a)] {
				return "runs systemctl " + a
			}
		}
	case powerShells[tool]:
		for _, a := range args {
			for _, tok := range strings.FieldsFunc(a, func(r rune) bool { return strings.ContainsRune(" \t\r\n;&|()", r) }) {
				if powerTools[baseTool(tok)] {
					return "runs the power tool " + baseTool(tok) + " through " + tool
				}
			}
		}
	}
	return ""
}

// problems applies the C-4 rules (a) to (c) to every scanned file.
func (s *powerScanner) problems() []string {
	for _, info := range s.infos {
		s.constsOf(info.dir)
	}
	var out []string
	flag := func(pos token.Pos, format string, args ...any) {
		out = append(out, fmt.Sprintf("%s: %s", s.fset.Position(pos), fmt.Sprintf(format, args...)))
	}
	for _, info := range s.infos {
		key := info.dir
		for _, decl := range info.file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && powerExemptFuncs[fn.Name.Name] && info.dir == "." && info.file.Name.Name == "agent" {
				continue
			}
			ast.Inspect(decl, func(n ast.Node) bool {
				switch x := n.(type) {
				case *ast.CallExpr:
					s.checkCall(x, info, key, flag)
				case *ast.SelectorExpr:
					if pkg, ok := x.X.(*ast.Ident); ok {
						name := x.Sel.Name
						switch info.imports[pkg.Name] {
						case "syscall", "golang.org/x/sys/unix":
							if name == "Reboot" || strings.HasPrefix(name, "LINUX_REBOOT_") || strings.HasPrefix(name, "RB_") {
								flag(x.Pos(), "calls the reboot system call %s.%s", pkg.Name, name)
							}
						case "golang.org/x/sys/windows":
							if name == "InitiateSystemShutdownEx" || name == "InitiateShutdown" || name == "ExitWindowsEx" {
								flag(x.Pos(), "calls the shutdown API %s.%s", pkg.Name, name)
							}
						}
					}
				}
				switch x := n.(type) {
				case *ast.BasicLit, *ast.BinaryExpr, *ast.Ident, *ast.SelectorExpr:
					if v, ok := s.fold(x.(ast.Expr), info, key); ok && powerStringRE.MatchString(v) {
						flag(n.Pos(), "has the string %q, which names a power action", v)
					}
				}
				return true
			})
		}
	}
	sort.Strings(out)
	return out
}

func (s *powerScanner) checkCall(call *ast.CallExpr, info *powerFileInfo, key string, flag func(token.Pos, string, ...any)) {
	name, pkgPath := "", ""
	switch f := call.Fun.(type) {
	case *ast.SelectorExpr:
		name = f.Sel.Name
		if pkg, ok := f.X.(*ast.Ident); ok {
			pkgPath = info.imports[pkg.Name]
		}
	case *ast.Ident:
		name = f.Name
	}
	launch := false
	switch pkgPath {
	case "os/exec":
		launch = name == "Command" || name == "CommandContext"
	case "os":
		launch = name == "StartProcess"
	case "syscall":
		launch = name == "Exec" || name == "ForkExec" || name == "StartProcess"
	}
	if name == "runner" || name == "runCommand" || name == "runUpgradeCommand" || runCommandRE.MatchString(name) {
		launch = true
	}
	if !launch {
		return
	}
	var folded []string
	for _, a := range flattenArgs(call.Args) {
		if v, ok := s.fold(a, info, key); ok {
			folded = append(folded, v)
		}
	}
	if len(folded) == 0 {
		return
	}
	program, rest := folded[0], folded[1:]
	if tool := baseTool(program); tool == "sudo" || tool == "doas" {
		// Skip the wrapper's own flags, then the next argument is the program.
		i := 0
		for i < len(rest) && strings.HasPrefix(rest[i], "-") {
			i++
		}
		if i >= len(rest) {
			return
		}
		program, rest = rest[i], rest[i+1:]
	}
	if why := powerLaunchProblem(program, rest); why != "" {
		flag(call.Pos(), "%s (%s)", why, name)
	}
}

// realPowerPathFiles lists every non-test Go file under internal/agent (".") and
// cmd/riot-agent, recursively.
func realPowerPathFiles(t *testing.T) []powerPathFile {
	t.Helper()
	var out []powerPathFile
	for _, root := range []string{".", filepath.Join("..", "..", "cmd", "riot-agent")} {
		err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
				return nil
			}
			src, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			out = append(out, powerPathFile{Path: filepath.ToSlash(p), Src: src})
			return nil
		})
		require.NoError(t, err, "walk %s", root)
	}
	return out
}

func TestPowerPaths_SD_C4_OnlyExecRebootAndExecShutdownLaunchPowerActions(t *testing.T) {
	files := realPowerPathFiles(t)

	var dirs []string
	for _, f := range files {
		dirs = append(dirs, path.Dir(f.Path))
	}
	require.Contains(t, dirs, ".", "[C-4] the scan must cover the agent package")
	require.Contains(t, dirs, "collectors", "[C-4] the scan must cover subpackages recursively")
	require.Contains(t, dirs, "../../cmd/riot-agent", "[C-4] the scan must cover the agent binary")

	problems := newPowerScanner(t, files).problems()

	assert.Empty(t, problems, "[C-4] nothing outside execReboot and execShutdown may launch a power tool, name a power verb or call a power system call")
}

func TestPowerPaths_SD_C4_SelfTest(t *testing.T) {
	flagged := []struct {
		name string
		path string
		src  string
	}{
		{"the Windows -s form in a subpackage file", "collectors/power_windows.go", `package collectors
import "os/exec"
func Off() error { return exec.Command("shutdown", "-s", "-t", "0").Start() }`},
		{"a Windows /s form", "other.go", `package agent
import "os/exec"
func off() error { return exec.Command("shutdown", "/s", "/t", "0").Start() }`},
		{"a path to shutdown behind sudo", "other.go", `package agent
import "os/exec"
func off() error { return exec.Command("sudo", "/sbin/shutdown", "-h", "now").Start() }`},
		{"sudo with its own flag", "other.go", `package agent
import "os/exec"
func off() error { return exec.Command("sudo", "-n", "/sbin/poweroff").Start() }`},
		{"systemctl start poweroff.target", "other.go", `package agent
import "os/exec"
func off() error { return exec.Command("systemctl", "start", "poweroff.target").Start() }`},
		{"systemctl halt", "other.go", `package agent
import "os/exec"
func off() error { return exec.Command("systemctl", "halt").Start() }`},
		{"the runner seam with a reboot outside execReboot", "other.go", `package agent
func (a *Agent) off(ctx context.Context) { a.runner(ctx, "sudo", "systemctl", "reboot") }`},
		{"runCommand with init 0", "other.go", `package agent
func (a *Agent) off(ctx context.Context) { runCommand(ctx, "init", "0") }`},
		{"a shell running telinit", "other.go", `package agent
import "os/exec"
func off() error { return exec.Command("sh", "-c", "sleep 1; telinit 0").Start() }`},
		{"a constant folded to poweroff", "other.go", `package agent
const verb = "power" + "off"
func off(a *Agent, ctx context.Context) { a.runner(ctx, "systemctl", verb) }`},
		{"the login1 PowerOff method", "other.go", `package agent
var m = "org.freedesktop.login1.Manager.PowerOff"`},
		{"unix.Reboot", "other.go", `package agent
import "golang.org/x/sys/unix"
func off() error { return unix.Reboot(unix.LINUX_REBOOT_CMD_POWER_OFF) }`},
		{"syscall.Reboot", "other.go", `package agent
import "syscall"
func off() error { return syscall.Reboot(0x4321fedc) }`},
		{"a power launch in another function of commands.go", "commands.go", `package agent
import "os/exec"
func (a *Agent) handleShutdown2() error { return exec.Command("sudo", "systemctl", "poweroff").Start() }`},
		{"a power launch in a func named execReboot of another package", "collectors/x.go", `package collectors
import "os/exec"
func (a *Agent) execReboot() error { return exec.Command("shutdown", "/r").Start() }`},
	}
	for _, tc := range flagged {
		t.Run("flags "+tc.name, func(t *testing.T) {
			problems := newPowerScanner(t, []powerPathFile{{Path: tc.path, Src: []byte(tc.src)}}).problems()
			assert.NotEmpty(t, problems, "[C-4] %s must be flagged", tc.name)
		})
	}

	allowed := []struct {
		name string
		path string
		src  string
	}{
		{"the real exec function shapes", "commands.go", `package agent
import (
	"context"
	"os/exec"
	"runtime"
)
func (a *Agent) execReboot() error {
	if a.runner != nil {
		_, err := a.runner(context.Background(), "sudo", "systemctl", "reboot")
		return err
	}
	var cmd *exec.Cmd
	if runtime.GOOS == "windows" {
		cmd = exec.Command("shutdown", "/r", "/t", "5")
	} else {
		cmd = exec.Command("sudo", "systemctl", "reboot")
	}
	return cmd.Start()
}
func (a *Agent) execShutdown() error {
	cmd := exec.Command("sudo", "systemctl", "poweroff")
	a.slog("shutdown: poweroff command failed after the start check")
	return cmd.Start()
}`},
		{"systemctl show", "other.go", `package agent
import "os/exec"
func probe() ([]byte, error) { return exec.Command("systemctl", "show", "x.service").Output() }`},
		{"the reboot-required marker path", "other.go", `package agent
var marker = "/var/run/reboot-required"`},
		{"the shutdown action name", "other.go", `package agent
import "github.com/DesyncTheThird/rIOt/internal/models"
var action = models.ActionShutdown`},
		{"an unrelated program", "other.go", `package agent
import "os/exec"
func list() ([]byte, error) { return exec.Command("sudo", "apt-get", "update").Output() }`},
	}
	for _, tc := range allowed {
		t.Run("allows "+tc.name, func(t *testing.T) {
			problems := newPowerScanner(t, []powerPathFile{{Path: tc.path, Src: []byte(tc.src)}}).problems()
			assert.Empty(t, problems, "[C-4] %s must not be flagged", tc.name)
		})
	}
}
