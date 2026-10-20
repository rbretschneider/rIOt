package maintenance

import (
	"encoding/hex"
	"errors"
	"time"
)

type UpdateStatus struct {
	Version   int       `json:"version"`
	State     string    `json:"state"`
	SHA256    string    `json:"sha256"`
	Message   string    `json:"message,omitempty"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (s UpdateStatus) Validate() error {
	if s.Version != ProtocolVersion || s.UpdatedAt.IsZero() || len(s.Message) > 4096 {
		return errors.New("invalid update handoff status")
	}
	if s.State != "accepted" && s.State != "deferred" && s.State != "error" && s.State != "installed" {
		return errors.New("invalid update state")
	}
	b, err := hex.DecodeString(s.SHA256)
	if err != nil || len(b) != 32 || hex.EncodeToString(b) != s.SHA256 {
		return errors.New("invalid update digest")
	}
	return nil
}
