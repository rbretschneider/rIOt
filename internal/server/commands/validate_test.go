package commands

import (
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/DesyncTheThird/rIOt/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validID = "0b6f1c3e-5a52-4e0c-9d57-3f8c2a4b7e10"

func TestCL_SEC001_SanitizeReportTable(t *testing.T) {
	longStep := strings.Repeat("a", 65)
	multibyte := strings.Repeat("é", 800) // 1600 bytes, every rune 2 bytes

	cases := []struct {
		name  string
		in    models.CommandReport
		check func(t *testing.T, out models.CommandReport)
	}{
		{
			name: "valid step is kept",
			in:   models.CommandReport{Kind: models.ReportKindProgress, Step: "stop_dependents"},
			check: func(t *testing.T, out models.CommandReport) {
				assert.Equal(t, "stop_dependents", out.Step)
			},
		},
		{
			name: "step over 64 bytes becomes empty",
			in:   models.CommandReport{Step: longStep},
			check: func(t *testing.T, out models.CommandReport) {
				assert.Equal(t, "", out.Step, "[SEC-001] an over-long step must be stored as empty")
			},
		},
		{
			name: "step with uppercase, space or slash becomes empty",
			in:   models.CommandReport{Step: "Pull Image/../x"},
			check: func(t *testing.T, out models.CommandReport) {
				assert.Equal(t, "", out.Step, "[SEC-001] a step outside [a-z0-9_] must be stored as empty")
			},
		},
		{
			name: "progress message cut to 1 KiB on a rune boundary",
			in:   models.CommandReport{Message: multibyte},
			check: func(t *testing.T, out models.CommandReport) {
				assert.LessOrEqual(t, len(out.Message), models.MaxProgressMessageBytes)
				assert.True(t, utf8.ValidString(out.Message), "[SEC-001] truncation must not split a rune")
				assert.Greater(t, len(out.Message), models.MaxProgressMessageBytes-4, "no more than one rune is lost")
			},
		},
		{
			name: "result reason outside the enum becomes empty",
			in:   models.CommandReport{Result: &models.CommandResult{Reason: "pwned"}},
			check: func(t *testing.T, out models.CommandReport) {
				assert.Equal(t, "", out.Result.Reason)
			},
		},
		{
			name: "result reason in the enum is kept",
			in:   models.CommandReport{Result: &models.CommandResult{Reason: models.ReasonRolledBack}},
			check: func(t *testing.T, out models.CommandReport) {
				assert.Equal(t, models.ReasonRolledBack, out.Result.Reason)
			},
		},
		{
			name: "output over 256 KiB keeps head and tail",
			in:   models.CommandReport{Result: &models.CommandResult{Output: "HEAD" + strings.Repeat("x", 2<<20) + "TAIL"}},
			check: func(t *testing.T, out models.CommandReport) {
				assert.LessOrEqual(t, len(out.Result.Output), models.MaxReportOutputBytes)
				assert.True(t, strings.HasPrefix(out.Result.Output, "HEAD"), "head is kept")
				assert.True(t, strings.HasSuffix(out.Result.Output, "TAIL"), "tail is kept")
				assert.Contains(t, out.Result.Output, models.TruncationMarker)
			},
		},
		{
			name: "stderr keeps the last 8 KiB",
			in:   models.CommandReport{Result: &models.CommandResult{Stderr: strings.Repeat("e", 20<<10) + "LAST"}},
			check: func(t *testing.T, out models.CommandReport) {
				assert.Len(t, out.Result.Stderr, models.MaxStderrTailBytes)
				assert.True(t, strings.HasSuffix(out.Result.Stderr, "LAST"), "[AC-094] the tail, not the head, is kept")
			},
		},
		{
			name: "result message keeps the last 64 KiB",
			in:   models.CommandReport{Result: &models.CommandResult{Message: strings.Repeat("m", 100<<10) + "END"}},
			check: func(t *testing.T, out models.CommandReport) {
				assert.Len(t, out.Result.Message, models.MaxResultMsgBytes)
				assert.True(t, strings.HasSuffix(out.Result.Message, "END"))
			},
		},
		{
			name: "result step follows the same rule as progress step",
			in:   models.CommandReport{Result: &models.CommandResult{Step: "recreate!"}},
			check: func(t *testing.T, out models.CommandReport) {
				assert.Equal(t, "", out.Result.Step)
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.check(t, sanitizeReport(tc.in))
		})
	}
}

func TestCL_SEC001_SanitizeReportDoesNotMutateInput(t *testing.T) {
	res := &models.CommandResult{Message: "a\x00b", Step: "BAD"}
	in := models.CommandReport{Kind: models.ReportKindResult, Step: "BAD", Message: "x\x00", Result: res}

	_ = sanitizeReport(in)

	assert.Equal(t, "a\x00b", res.Message, "the caller's result must be left alone")
	assert.Equal(t, "BAD", res.Step)
	assert.Equal(t, "BAD", in.Step)
}

func TestCL_SEC002_SanitizeNULAndInvalidUTF8(t *testing.T) {
	hostile := "start\x00mid\xff\xfeend\x00"
	out := sanitizeReport(models.CommandReport{
		Message: hostile,
		Result: &models.CommandResult{
			Message: hostile, Stderr: hostile, Output: hostile,
		},
	})

	for name, v := range map[string]string{
		"progress message": out.Message, "result message": out.Result.Message,
		"stderr": out.Result.Stderr, "output": out.Result.Output,
	} {
		assert.NotContains(t, v, "\x00", "[SEC-002] %s must be NUL-free", name)
		assert.True(t, utf8.ValidString(v), "[SEC-002] %s must be valid UTF-8", name)
		assert.Contains(t, v, "start", "[SEC-002] content is kept, not rejected")
		assert.Contains(t, v, "end")
	}
}

func TestValidateReport(t *testing.T) {
	ok := func(r models.CommandReport) models.CommandReport { return r }
	cases := []struct {
		name    string
		pathID  string
		report  models.CommandReport
		wantErr error
	}{
		{"ack", validID, ok(models.CommandReport{Kind: models.ReportKindAck}), nil},
		{"ack with matching body id", validID, models.CommandReport{Kind: models.ReportKindAck, CommandID: validID}, nil},
		{"progress running", validID, models.CommandReport{Kind: models.ReportKindProgress, State: "running"}, nil},
		{"progress verifying", validID, models.CommandReport{Kind: models.ReportKindProgress, State: "verifying"}, nil},
		{"result", validID, models.CommandReport{Kind: models.ReportKindResult, Result: &models.CommandResult{}}, nil},
		{"path id not a uuid", "../../telemetry", models.CommandReport{Kind: models.ReportKindAck}, ErrMalformedCommandID},
		{"uppercase uuid", strings.ToUpper(validID), models.CommandReport{Kind: models.ReportKindAck}, ErrMalformedCommandID},
		{"1 MiB id", strings.Repeat("a", 1<<20), models.CommandReport{Kind: models.ReportKindAck}, ErrMalformedCommandID},
		{"body id mismatch", validID, models.CommandReport{Kind: models.ReportKindAck, CommandID: "1b6f1c3e-5a52-4e0c-9d57-3f8c2a4b7e10"}, ErrInvalidReport},
		{"unknown kind", validID, models.CommandReport{Kind: "cancel"}, ErrInvalidReport},
		{"empty kind", validID, models.CommandReport{}, ErrInvalidReport},
		{"progress with ack state", validID, models.CommandReport{Kind: models.ReportKindProgress, State: "acknowledged"}, ErrInvalidReport},
		{"progress without state", validID, models.CommandReport{Kind: models.ReportKindProgress}, ErrInvalidReport},
		{"result without result", validID, models.CommandReport{Kind: models.ReportKindResult}, ErrInvalidReport},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := ValidateReport(tc.pathID, tc.report)
			if tc.wantErr == nil {
				require.NoError(t, err)
				return
			}
			require.ErrorIs(t, err, tc.wantErr)
		})
	}
}
