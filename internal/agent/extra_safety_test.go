package agent

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// EXTRA-UPD-001 AC-045 and SC-001/SC-002/SC-004. The scan reads every Go source
// file of the repository and looks at the places where a test or a helper could
// start a process. It is AST-only, like the power-path scan, and it cannot see
// a command built at run time from data; that residual is why every probe in the
// extra-updater tests is an injected fake and every real-process test starts
// only /bin/sh fixtures or the test binary itself.

var (
	forbiddenTools = map[string]bool{
		"reboot": true, "shutdown": true, "poweroff": true, "halt": true, "kexec": true, "telinit": true,
		"riot-agent": true, "dietpi-update": true, "dietpi-software": true, "dietpi-services": true, "dietpi-set_software": true,
	}
	mutatingPackageTools = map[string]bool{"apt": true, "apt-get": true, "dpkg": true, "apt-mark": true, "dnf": true, "yum": true}
	mutatingVerbs        = map[string]bool{"install": true, "upgrade": true, "dist-upgrade": true, "full-upgrade": true, "remove": true, "purge": true, "autoremove": true, "--configure": true, "hold": true, "unhold": true, "update": true, "-i": true, "-r": true, "-P": true}
	processStarters      = map[string]map[string]bool{
		"os/exec": {"Command": true, "CommandContext": true},
		"syscall": {"Exec": true, "ForkExec": true},
		"os":      {"StartProcess": true},
	}
)

type scannedFile struct {
	path    string
	isTest  bool
	file    *ast.File
	imports map[string]string
}

func repoGoFiles(t *testing.T) []scannedFile {
	t.Helper()
	root := filepath.Join("..", "..")
	fset := token.NewFileSet()
	var files []scannedFile
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", "web", ".claude":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") {
			return nil
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			return nil // a file this scan cannot parse cannot be a Go test either
		}
		sf := scannedFile{path: filepath.ToSlash(p), isTest: strings.HasSuffix(p, "_test.go"), file: f, imports: map[string]string{}}
		for _, im := range f.Imports {
			ip, _ := strconv.Unquote(im.Path.Value)
			alias := path.Base(ip)
			if im.Name != nil {
				alias = im.Name.Name
			}
			sf.imports[alias] = ip
		}
		files = append(files, sf)
		return nil
	})
	require.NoError(t, err)
	require.NotEmpty(t, files)
	return files
}

// literalStrings returns the string literals among a call's arguments, including
// the elements of a literal []string argument.
func literalStrings(args []ast.Expr) []string {
	var out []string
	var visit func(ast.Expr)
	visit = func(e ast.Expr) {
		switch v := e.(type) {
		case *ast.BasicLit:
			if v.Kind == token.STRING {
				if s, err := strconv.Unquote(v.Value); err == nil {
					out = append(out, s)
				}
			}
		case *ast.CompositeLit:
			for _, el := range v.Elts {
				visit(el)
			}
		case *ast.BinaryExpr:
			visit(v.X)
			visit(v.Y)
		}
	}
	for _, a := range args {
		visit(a)
	}
	return out
}

func forbiddenProcess(literals []string) (string, bool) {
	if len(literals) == 0 {
		return "", false
	}
	tool := path.Base(strings.ReplaceAll(literals[0], `\`, "/"))
	switch {
	case forbiddenTools[tool], strings.HasPrefix(tool, "dietpi-"):
		return tool, true
	case mutatingPackageTools[tool]:
		for _, arg := range literals[1:] {
			if mutatingVerbs[arg] {
				return tool + " " + arg, true
			}
		}
	}
	// A shell command line that names a forbidden tool.
	for _, lit := range literals {
		if strings.Contains(lit, "dietpi-update") || strings.Contains(lit, "dietpi-software") {
			return lit, true
		}
	}
	return "", false
}

func TestSafety_EU_AC045_NoForbiddenProcess(t *testing.T) {
	files := repoGoFiles(t)
	started := 0
	for _, sf := range files {
		ast.Inspect(sf.file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			pkg, ok := sel.X.(*ast.Ident)
			if !ok {
				return true
			}
			if funcs := processStarters[sf.imports[pkg.Name]]; funcs != nil && funcs[sel.Sel.Name] {
				started++
				if what, bad := forbiddenProcess(literalStrings(call.Args)); bad && sf.isTest {
					t.Errorf("[AC-045] %s starts %q: tests must use injected fakes only (SC-001)", sf.path, what)
				}
			}
			return true
		})
	}
	assert.Greater(t, started, 5, "the scan must actually see process starts, or it proves nothing")
}

// A test that calls the launcher must never point it at a DietPi program, and a
// fixture dietpi-update must never be written with an execute bit.
func TestSafety_EU_AC045_FixturesAreTripwiresAndLauncherNeverSeesDietPi(t *testing.T) {
	for _, sf := range repoGoFiles(t) {
		if !sf.isTest {
			continue
		}
		ast.Inspect(sf.file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			name := ""
			switch fn := call.Fun.(type) {
			case *ast.Ident:
				name = fn.Name
			case *ast.SelectorExpr:
				name = fn.Sel.Name
			}
			literals := literalStrings(call.Args)
			switch name {
			case "RunExtra", "runExtra", "RunExtraMain":
				for _, lit := range literals {
					if strings.Contains(lit, "/boot/dietpi") {
						t.Errorf("[AC-045] %s calls %s with %q", sf.path, name, lit)
					}
				}
			case "WriteFile", "writeFixture", "writeHost", "write":
				for _, lit := range literals {
					if strings.Contains(lit, "dietpi-update") {
						for _, arg := range call.Args {
							if bl, ok := arg.(*ast.BasicLit); ok && bl.Kind == token.INT {
								mode, err := strconv.ParseInt(bl.Value, 0, 64)
								if err == nil && mode&0o111 != 0 {
									t.Errorf("[AC-045] %s writes a dietpi-update fixture with execute bits %o", sf.path, mode)
								}
							}
						}
					}
				}
			}
			return true
		})
	}
}

// Detection tests read fixture trees only and make no network request (SC-004).
func TestSafety_EU_AC045_DetectionTestsUseNoNetworkAndNoRealBoot(t *testing.T) {
	for _, sf := range repoGoFiles(t) {
		dir := path.Dir(sf.path)
		if !strings.HasSuffix(dir, "internal/extraupd") {
			continue
		}
		for alias, ip := range sf.imports {
			if ip == "net/http" || ip == "net" || strings.HasPrefix(ip, "net/http/") {
				t.Errorf("[SC-004] %s imports %s (%s): extraupd tests must not touch the network", sf.path, ip, alias)
			}
		}
		ast.Inspect(sf.file, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if pkg, ok := sel.X.(*ast.Ident); ok && sf.imports[pkg.Name] == "os" {
				for _, lit := range literalStrings(call.Args) {
					if strings.HasPrefix(lit, "/boot") || strings.HasPrefix(lit, "/run/dietpi") {
						t.Errorf("[SC-004] %s calls os.%s on %q: only fixture trees may be read", sf.path, sel.Sel.Name, lit)
					}
				}
			}
			return true
		})
	}
}

// The extra-updater step reports through the command and event path only. It
// must not depend on EXPECT-001, which is unmerged (FR-046).
func TestNoExpectCode_EU_AC034(t *testing.T) {
	root := filepath.Join("..", "..")
	var offenders []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			switch d.Name() {
			case ".git", "node_modules", ".claude", "docs":
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		if strings.Contains(strings.ToLower(string(data)), "internal/expect") || strings.Contains(string(data), "ExpectedActivity") {
			offenders = append(offenders, filepath.ToSlash(p))
		}
		return nil
	})
	require.NoError(t, err)
	assert.Empty(t, offenders, "[AC-034] the build contains no EXPECT-001 code: %v", offenders)
	_, err = os.Stat(filepath.Join(root, "internal", "expect"))
	assert.True(t, os.IsNotExist(err), "[AC-034] there is no internal/expect package in this build")
}
