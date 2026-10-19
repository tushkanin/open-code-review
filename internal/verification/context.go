// SPDX-License-Identifier: Apache-2.0
// Copyright 2026 alibaba/open-code-review Contributors

package verification

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/alibaba/open-code-review/internal/diff"
	"github.com/alibaba/open-code-review/internal/model"
)

const ContextVersion = 1

type Mode string

const (
	Enabled  Mode = "enabled"
	Disabled Mode = "disabled"
)

// ContextInputs excludes execution provenance and random invocation identifiers.
type ContextInputs struct {
	Diffs               []model.Diff `json:"diffs"`
	GroupContext        string       `json:"group_context"`
	Scope               string       `json:"scope"`
	Rules               string       `json:"rules"`
	Background          string       `json:"background"`
	ContractFingerprint string       `json:"contract_fingerprint"`
	Mode                Mode         `json:"mode"`
}

type Context struct {
	Version       int                  `json:"version"`
	Input         diff.InputResolution `json:"input"`
	EvidenceState string               `json:"evidence_state"`
	ContextInputs
}

func NewContext(view *diff.EvidenceView, inputs ContextInputs) (Context, error) {
	if view == nil {
		return Context{}, errors.New("verification context requires a captured view")
	}
	inputs.Diffs = append([]model.Diff{}, inputs.Diffs...)
	result := Context{Version: ContextVersion, Input: view.Input(), EvidenceState: view.State(), ContextInputs: inputs}
	_, err := result.Identity()
	return result, err
}

func (c Context) Identity() (string, error) {
	if c.Version != ContextVersion || c.EvidenceState == "" || (c.Mode != Enabled && c.Mode != Disabled) || c.ContractFingerprint == "" {
		return "", errors.New("unsupported or incomplete verification context")
	}
	data, err := json.Marshal(c)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), nil
}

func (c Context) Matches(view *diff.EvidenceView) bool {
	return view != nil && c.Input == view.Input() && c.EvidenceState == view.State()
}
