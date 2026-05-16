// Package buffer is the hook's local event queue.
//
// The hook is invoked by an agent's PreToolUse machinery — it MUST
// return within a few ms or the user feels the agent lagging. So we
// append the event to a small JSON-lines file in ~/.oryxai/buffer.jsonl
// and exit. A daemon (or the next install/verify command) drains the
// file to the control plane in the background.
//
// File format: one JSON-encoded FeedEvent per line. No header, no
// metadata — recovery from corruption is just "skip bad lines."
//
// Concurrency: appended writes via os.OpenFile with O_APPEND are
// atomic up to PIPE_BUF on POSIX (4 KB on Linux, 512 B on macOS).
// Our event JSON is ~200-300 bytes, well under either limit. So we
// don't need any locking — multiple hook invocations write
// concurrently without interleaving.
package buffer

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/OryxAIcode/Oryxai/cmd/oryxai/internal/client"
)

const bufferFile = "buffer.jsonl"

// path returns the absolute buffer file path in the user's oryxai dir.
func path() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".oryxai", bufferFile), nil
}

// Append writes one event to the buffer. Trivial cost: open(append),
// write, close. Returns nil on any I/O failure (we don't want the
// hook to block on a failed append).
//
// Caller: the hook command, on every tool call.
func Append(evt client.FeedEvent) error {
	p, err := path()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(p, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer f.Close()
	enc, err := json.Marshal(evt)
	if err != nil {
		return err
	}
	enc = append(enc, '\n')
	if len(enc) > 4096 {
		// Defensive: if an event is bigger than PIPE_BUF on Linux,
		// drop it rather than risk interleaved writes. Shouldn't
		// happen with our event shape — but if a future field push
		// goes wild we'd rather lose one event than corrupt the file.
		return fmt.Errorf("event too large: %d bytes", len(enc))
	}
	_, err = f.Write(enc)
	return err
}

// Drain reads the buffer, ships everything via c.Ingest in batches,
// and truncates on success. On partial failure (network down,
// partial batch rejected), leaves unsent events in place for the next
// drain attempt.
//
// Caller: install/verify/status — opportunistic drain whenever the
// CLI runs. Future: a background daemon could call this on a timer.
func Drain(c *client.Client, batchSize int) (sent int, err error) {
	if batchSize <= 0 || batchSize > 100 {
		batchSize = 100
	}
	p, err := path()
	if err != nil {
		return 0, err
	}
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()

	var batch []client.FeedEvent
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 8*1024), 16*1024)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		resp, err := c.Ingest(ctx, batch)
		if err != nil {
			return err
		}
		sent += resp.Accepted
		batch = batch[:0]
		return nil
	}
	for sc.Scan() {
		var evt client.FeedEvent
		if err := json.Unmarshal(sc.Bytes(), &evt); err != nil {
			continue // skip corrupt lines
		}
		batch = append(batch, evt)
		if len(batch) >= batchSize {
			if err := flush(); err != nil {
				return sent, err
			}
		}
	}
	if err := sc.Err(); err != nil {
		return sent, err
	}
	if err := flush(); err != nil {
		return sent, err
	}
	// All events shipped successfully — truncate the file. We
	// re-open it for writing and immediately close: O_TRUNC clears
	// content while preserving inode + perms.
	if err := os.Truncate(p, 0); err != nil {
		return sent, fmt.Errorf("truncate buffer: %w", err)
	}
	return sent, nil
}

// Count reports how many events are queued without sending. Useful
// for `oryxai status` to show "N events pending upload."
func Count() (int, error) {
	p, err := path()
	if err != nil {
		return 0, err
	}
	f, err := os.Open(p)
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 8*1024), 16*1024)
	n := 0
	for sc.Scan() {
		if len(sc.Bytes()) > 0 {
			n++
		}
	}
	return n, sc.Err()
}
