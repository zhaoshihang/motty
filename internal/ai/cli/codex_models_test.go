package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"motty/internal/ai/llm"
	"motty/internal/ai/store"
)

func TestCodexModelListUsesPickerIDsAcrossPages(t *testing.T) {
	responses := `{"id":1,"result":{}}
{"method":"configWarning","params":{}}
{"id":2,"result":{"data":[{"id":"picker-row","model":"model-a","displayName":"Model A"},{"model":"hidden","hidden":true},{"model":"default"}],"nextCursor":"page 2"}}
{"id":3,"result":{"data":[{"model":"model-a"},{"id":"model-b"},{}],"nextCursor":null}}`
	var requests bytes.Buffer
	names, err := readCodexModels(&requests, strings.NewReader(responses))
	if err != nil || !slices.Equal(names, []string{"default", "model-a", "model-b"}) {
		t.Fatalf("models = %v, err = %v", names, err)
	}
	decoder := json.NewDecoder(&requests)
	for index, method := range []string{"initialize", "initialized", "model/list", "model/list"} {
		var request struct {
			Method string
			ID     int
			Params struct {
				Cursor        string
				IncludeHidden bool
				Limit         int
			}
		}
		if err := decoder.Decode(&request); err != nil || request.Method != method {
			t.Fatalf("request %d = %+v, err = %v", index, request, err)
		}
		if method == "model/list" && (request.ID != index || request.Params.IncludeHidden || request.Params.Limit <= 0) {
			t.Fatalf("unexpected picker request: %+v", request)
		}
		if (index == 2 && request.Params.Cursor != "") || (index == 3 && request.Params.Cursor != "page 2") {
			t.Fatalf("page %d cursor = %q", index-1, request.Params.Cursor)
		}
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		t.Fatal("model discovery sent another request", err)
	}
	cmd, err := buildCommand(store.KindCodexCLI, command{program: "codex"}, llm.Request{Model: names[1]}, t.TempDir(), "http://127.0.0.1:1/mcp", "test", nil, "motty_test")
	i := slices.Index(cmd.args, "--model")
	if err != nil || i < 0 || i+1 >= len(cmd.args) || cmd.args[i+1] != "model-a" {
		t.Fatal("Codex exec did not receive the model selected from its catalog", err)
	}
}

func TestCodexModelListRejectsIncompleteCatalog(t *testing.T) {
	const first = `{"id":1,"result":{}}` + "\n"
	for _, response := range []string{
		`{"id":1,"error":{"code":-32601,"message":"private detail"}}`,
		first + `{"id":2,"result":{}}`,
		first + `{"id":2,"result":{"data":[{"model":"partial"}],"nextCursor":"more"}}`,
		first + `{"id":2,"result":{"data":[],"nextCursor":"more"}}` + "\n" + `{"id":3,"result":{"data":[],"nextCursor":"more"}}`,
	} {
		names, err := readCodexModels(io.Discard, strings.NewReader(response))
		if names != nil || !errors.Is(err, ErrModelCatalog) {
			t.Fatalf("unexpected catalog result %v, %v", names, err)
		}
	}
	names, err := readCodexModels(io.Discard, strings.NewReader(first+`{"id":2,"result":{"data":[]}}`))
	if err != nil || !slices.Equal(names, []string{"default"}) {
		t.Fatalf("empty visible catalog = %v, %v", names, err)
	}
}
