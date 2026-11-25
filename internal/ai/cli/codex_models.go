package cli

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strconv"
	"strings"
	"time"

	"motty/internal/update"
)

// ErrModelCatalog means the CLI passed its compatibility checks but could not
// list models. Its default model and manually configured IDs can still be used.
var ErrModelCatalog = errors.New("cli: model_catalog_unavailable")

// codexModels asks a private stdio app-server for the same catalog as Codex's
// model picker. No thread or inference turn is needed.
func codexModels(parent context.Context, c command) ([]string, error) {
	ctx, cancel := context.WithTimeout(parent, 15*time.Second)
	defer cancel()
	c.args = append(c.args, "app-server")
	cmd := newCLIProcess(ctx, c)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, ErrModelCatalog
	}
	defer in.Close()
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, ErrModelCatalog
	}
	defer out.Close()
	if err := cmd.Start(); err != nil {
		if parent.Err() != nil {
			return nil, parent.Err()
		}
		return nil, ErrModelCatalog
	}
	stopRead := context.AfterFunc(ctx, func() { out.Close() })
	defer stopRead()
	names, err := readCodexModels(in, out)
	in.Close() // EOF asks app-server to shut down.
	if err != nil {
		cancel()
	}
	waitErr := cmd.Wait()
	if parent.Err() != nil {
		return nil, parent.Err()
	}
	if err != nil || waitErr != nil {
		return nil, ErrModelCatalog
	}
	return names, nil
}

func readCodexModels(in io.Writer, out io.Reader) ([]string, error) {
	send := json.NewEncoder(in).Encode
	receive := json.NewDecoder(io.LimitReader(out, maxOutput))
	client := map[string]string{"name": "motty", "version": update.Current}
	if err := send(map[string]any{"method": "initialize", "id": 1, "params": map[string]any{"clientInfo": client}}); err != nil {
		return nil, ErrModelCatalog
	}
	if _, err := codexCatalogReply(receive, 1); err != nil {
		return nil, err
	}
	if err := send(map[string]any{"method": "initialized", "params": struct{}{}}); err != nil {
		return nil, ErrModelCatalog
	}

	names := []string{"default"}
	seen := map[string]bool{"default": true}
	cursors := make(map[string]bool)
	params := map[string]any{"limit": 50, "includeHidden": false}
	for id := 2; ; id++ {
		if err := send(map[string]any{"method": "model/list", "id": id, "params": params}); err != nil {
			return nil, ErrModelCatalog
		}
		reply, err := codexCatalogReply(receive, id)
		if err != nil {
			return nil, err
		}
		var page struct {
			Data []struct {
				Model, ID string
				Hidden    bool
			}
			NextCursor string
		}
		if json.Unmarshal(reply, &page) != nil || page.Data == nil {
			return nil, ErrModelCatalog
		}
		for _, model := range page.Data {
			name := strings.TrimSpace(model.Model)
			if name == "" {
				name = strings.TrimSpace(model.ID)
			}
			if !model.Hidden && name != "" && !seen[name] {
				names = append(names, name)
				seen[name] = true
			}
		}
		if page.NextCursor == "" {
			return names, nil
		}
		if cursors[page.NextCursor] {
			return nil, ErrModelCatalog
		}
		cursors[page.NextCursor] = true
		params["cursor"] = page.NextCursor
	}
}

// Notifications can arrive between replies. Only consume the result for the
// outstanding request, and keep raw CLI errors out of the settings UI.
func codexCatalogReply(receive *json.Decoder, id int) (json.RawMessage, error) {
	for {
		var message struct {
			ID, Result, Error json.RawMessage
			Method            string
		}
		if receive.Decode(&message) != nil {
			return nil, ErrModelCatalog
		}
		if message.Method != "" || string(message.ID) != strconv.Itoa(id) {
			continue
		}
		if len(message.Error) != 0 || len(message.Result) == 0 || string(message.Result) == "null" {
			return nil, ErrModelCatalog
		}
		return message.Result, nil
	}
}
