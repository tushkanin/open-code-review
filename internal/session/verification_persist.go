// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package session

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

func (jw *jsonlWriter) writeVerification(kind string, fields map[string]any) error {
	jw.mu.Lock()
	defer jw.mu.Unlock()
	if jw.verificationErr != nil {
		return jw.verificationErr
	}
	id := generateUUID()
	record := map[string]any{"uuid": id, "parentUuid": jw.lastUUID, "type": kind, "sessionId": jw.sessionID, "timestamp": time.Now().UTC().Format(time.RFC3339)}
	for key, value := range fields {
		record[key] = value
	}
	data, err := json.Marshal(record)
	if err != nil {
		err = fmt.Errorf("marshal %s: %w", kind, err)
	} else if jw.writer == nil {
		err = errors.New("verification persistence is unavailable")
	} else if _, err = jw.writer.Write(data); err != nil {
		err = fmt.Errorf("write %s: %w", kind, err)
	} else if err = jw.writer.WriteByte('\n'); err != nil {
		err = fmt.Errorf("write %s newline: %w", kind, err)
	} else if err = jw.writer.Flush(); err != nil {
		err = fmt.Errorf("flush %s: %w", kind, err)
	}
	if err != nil {
		jw.verificationErr = err
		return err
	}
	jw.lastUUID = id
	return nil
}
