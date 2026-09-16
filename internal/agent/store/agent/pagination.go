package agent

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
)

// ErrInvalidPageToken is returned when page_token cannot be decoded or validated.
var ErrInvalidPageToken = errors.New("store: invalid page_token")

type pageTokenPayload struct {
	Offset int `json:"offset"`
}

func decodePageToken(token string) (int, error) {
	decoded, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return 0, fmt.Errorf("%w: %v", ErrInvalidPageToken, err)
	}

	var payload pageTokenPayload
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return 0, fmt.Errorf("%w: %v", ErrInvalidPageToken, err)
	}
	if payload.Offset < 0 {
		return 0, fmt.Errorf("%w: negative offset", ErrInvalidPageToken)
	}
	return payload.Offset, nil
}

func encodePageToken(offset int) (string, error) {
	if offset < 0 {
		return "", fmt.Errorf("%w: negative offset", ErrInvalidPageToken)
	}
	payload, err := json.Marshal(pageTokenPayload{Offset: offset})
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(payload), nil
}
