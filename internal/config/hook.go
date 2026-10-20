package config

import (
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/pelletier/go-toml/v2"
)

// HookCaptureSnippets reads only the hook's privacy setting. It avoids default
// construction, environment enumeration, path expansion and unrelated validation.
func HookCaptureSnippets() (bool, error) {
	if value, ok := os.LookupEnv("DRIGA_CAPTURE_TEXT"); ok {
		if value != "titles" && value != "snippets" {
			return false, errors.New("invalid capture_text")
		}
		return value == "snippets", nil
	}
	f, err := os.Open(Path())
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	// A hook must not spend unbounded time parsing configuration.
	b, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	if err != nil {
		return false, err
	}
	if len(b) > 1<<20 {
		return false, errors.New("hook configuration exceeds 1 MiB")
	}
	var policy struct {
		CaptureText string `toml:"capture_text"`
	}
	if err := toml.Unmarshal(b, &policy); err != nil {
		return false, fmt.Errorf("hook configuration: %w", err)
	}
	if policy.CaptureText != "" && policy.CaptureText != "titles" && policy.CaptureText != "snippets" {
		return false, errors.New("invalid capture_text")
	}
	return policy.CaptureText == "snippets", nil
}
