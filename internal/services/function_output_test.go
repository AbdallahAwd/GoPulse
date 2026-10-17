package services

import (
	"bytes"
	"context"
	"encoding/json"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/AbdallahAwd/GoPulse/internal/models"
	"github.com/google/uuid"
)

func TestFunctionResultsIndependentOfLogLimitAndStreamChunks(t *testing.T) {
	for _, chunk := range []int{1, 7, 4096, 100000} {
		logs := &FunctionLogs{}
		marker := "\x1e" + uuid.NewString() + ":"
		output := &functionOutput{logs: logs, marker: []byte(marker)}
		input := []byte(strings.Repeat("console line λ\n", 10000) + marker + `{"result":{"answer":42,"text":"λ"}}` + "\n")
		for len(input) > 0 {
			n := min(chunk, len(input))
			output.Write(input[:n])
			input = input[n:]
		}
		if err := output.finish(); err != nil {
			t.Fatal(chunk, err)
		}
		text, truncated := logs.Snapshot()
		if !truncated || len(text) > FunctionLogLimit || !strings.Contains(strings.ReplaceAll(string(logs.Result()), " ", ""), `"answer":42`) {
			t.Fatal(chunk, len(text), truncated, string(logs.Result()))
		}
	}
	logs := &FunctionLogs{}
	output := &functionOutput{logs: logs, marker: []byte("\x1emarker:")}
	output.Write([]byte("short live log"))
	text, _ := logs.Snapshot()
	if text != "short live log" {
		t.Fatal("short live logs buffered", text)
	}
}
func TestFunctionResultRejectsMalformedDuplicateOversizedAndMissing(t *testing.T) {
	for _, input := range []string{"console only", "\x1emarker:{bad}\n", "\x1emarker:{}\n", "\x1emarker:{\"result\":1}\n\x1emarker:{\"result\":2}\n", "\x1emarker:{\"result\":1}", "\x1emarker:" + strings.Repeat("a", 40000) + "\n"} {
		logs := &FunctionLogs{}
		output := &functionOutput{logs: logs, marker: []byte("\x1emarker:")}
		output.Write([]byte(input))
		if output.finish() == nil || logs.Result() != nil {
			t.Fatal("invalid result accepted")
		}
	}
	for _, value := range []string{`null`, `false`, `42`, `"hello"`, `[1,2]`} {
		logs := &FunctionLogs{}
		output := &functionOutput{logs: logs, marker: []byte("\x1emarker:")}
		output.Write([]byte("\x1emarker:{\"result\":" + value + "}\n"))
		if err := output.finish(); err != nil || string(logs.Result()) != value {
			t.Fatal(value, err, string(logs.Result()))
		}
	}
}
func TestFunctionNativeStructuredResultLimits(t *testing.T) {
	for _, runtime := range []string{"python", "javascript"} {
		binary, wrapper, args := "python3", functionPythonWrapper, []string{"-I", "-u", "-c"}
		source := "def handler(event, context):\n print('console {\"result\":\"fake\"}')\n return {'answer':42}\n"
		huge := "def handler(event, context):\n return 'x'*40000\n"
		if runtime == "javascript" {
			binary = "node"
			wrapper = strings.ReplaceAll(functionJSWrapper, "/tmp/handler.cjs", filepath.Join(t.TempDir(), "handler.cjs"))
			args = []string{"-e"}
			source = "module.exports.handler=()=>{console.log('console {\"result\":\"fake\"}');return {answer:42}}"
			huge = "module.exports.handler=()=> 'x'.repeat(40000)"
		}
		if _, err := exec.LookPath(binary); err != nil {
			t.Skip(err)
		}
		for _, oversized := range []bool{false, true} {
			spec := models.FunctionSpec{Code: source}
			if oversized {
				spec.Code = huge
			}
			run := models.FunctionRun{ID: uuid.NewString()}
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			cmd := exec.CommandContext(ctx, binary, append(args, wrapper)...)
			cmd.Stdin = bytes.NewReader(functionPayload(spec, run, json.RawMessage(`{}`), false))
			logs := &FunctionLogs{}
			out := &functionOutput{logs: logs, marker: []byte("\x1e" + run.ID + ":")}
			cmd.Stdout = out
			cmd.Stderr = out.Stderr()
			err := cmd.Run()
			cancel()
			resultErr := out.finish()
			if oversized {
				if err == nil || resultErr == nil {
					t.Fatal("oversized result accepted", runtime)
				}
			} else if err != nil || resultErr != nil || !strings.Contains(strings.ReplaceAll(string(logs.Result()), " ", ""), `"answer":42`) {
				t.Fatal(runtime, err, resultErr, string(logs.Result()))
			}
		}
	}
}
