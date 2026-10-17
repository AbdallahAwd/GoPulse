package services

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"sync"
)

// The wrapper emits a framed result independently of the bounded console log.
// All returned values remain untrusted function data, never authorization.
type functionOutput struct {
	mu                  sync.Mutex
	logs                io.Writer
	marker, pending     []byte
	collecting, invalid bool
	result              json.RawMessage
}

func (o *functionOutput) Stderr() io.Writer { return o.logs }

func (o *functionOutput) Write(p []byte) (int, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	n := len(p)
	for len(p) > 0 {
		size := min(len(p), 4096)
		o.pending = append(o.pending, p[:size]...)
		p = p[size:]
		o.consume()
	}
	return n, nil
}
func (o *functionOutput) consume() {
	for {
		if o.collecting {
			end := bytes.IndexByte(o.pending, '\n')
			if end < 0 {
				if len(o.pending) > 32768 {
					o.invalid = true
					o.pending = nil
					o.collecting = false
				}
				return
			}
			raw := o.pending[:end]
			var envelope map[string]json.RawMessage
			if len(raw) > 32768 || json.Unmarshal(raw, &envelope) != nil || len(envelope) != 1 || envelope["result"] == nil || o.result != nil {
				o.invalid = true
			} else {
				o.result = append(json.RawMessage(nil), envelope["result"]...)
			}
			o.logs.Write(raw)
			o.logs.Write([]byte("\n"))
			o.pending = o.pending[end+1:]
			o.collecting = false
			continue
		}
		at := bytes.Index(o.pending, o.marker)
		if at >= 0 {
			o.logs.Write(o.pending[:at])
			o.pending = o.pending[at+len(o.marker):]
			o.collecting = true
			continue
		}
		keep := min(len(o.pending), len(o.marker)-1)
		for keep > 0 && !bytes.Equal(o.pending[len(o.pending)-keep:], o.marker[:keep]) {
			keep--
		}
		safe := len(o.pending) - keep
		if safe > 0 {
			o.logs.Write(o.pending[:safe])
			o.pending = append(o.pending[:0], o.pending[safe:]...)
		}
		return
	}
}
func (o *functionOutput) finish() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.logs.Write(o.pending)
	o.pending = nil
	if o.invalid || o.collecting || o.result == nil {
		return errors.New("Function did not return one valid JSON result up to 32 KiB")
	}
	if sink, ok := o.logs.(interface{ SetResult(json.RawMessage) }); ok {
		sink.SetResult(o.result)
	}
	return nil
}
