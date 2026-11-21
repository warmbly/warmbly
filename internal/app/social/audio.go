package social

import (
	"context"
	"fmt"
	"mime"
	"strings"

	"github.com/google/uuid"

	"github.com/warmbly/warmbly/internal/pkg/graphkit"
)

// audioTypes are the recordings GraphKit takes for voice notes.
var audioTypes = map[string]bool{
	"audio/mpeg": true, "audio/mp3": true, "audio/mp4": true, "audio/x-m4a": true, "audio/m4a": true, "audio/aac": true,
	"audio/ogg": true, "audio/opus": true, "audio/webm": true, "audio/wav": true, "audio/x-wav": true, "audio/wave": true,
}

// AudioUpload is POST /v1/social/audio: the recording and its length, which GraphKit cannot measure.
type AudioUpload struct {
	Filename        string
	ContentType     string
	Data            []byte
	DurationSeconds int
}

// UploadAudio stores a recording for voice notes in the workspace's GraphKit audio storage.
func (s *Service) UploadAudio(ctx context.Context, orgID uuid.UUID, in AudioUpload) (*graphkit.Audio, error) {
	if err := s.ready(ctx, orgID); err != nil {
		return nil, err
	}
	ct, _, err := mime.ParseMediaType(strings.TrimSpace(in.ContentType))
	if err != nil || !audioTypes[strings.ToLower(ct)] {
		return nil, invalid("The recording must be MP3, M4A, OGG, WebM or WAV.")
	}
	switch {
	case len(in.Data) == 0:
		return nil, invalid("The recording is empty.")
	case len(in.Data) > graphkit.AudioMaxBytes:
		return nil, invalid("The recording must be at most 16 MiB.")
	case in.DurationSeconds < 1 || in.DurationSeconds > graphkit.AudioMaxSeconds:
		return nil, invalid(fmt.Sprintf("duration_seconds must be between 1 and %d.", graphkit.AudioMaxSeconds))
	}
	name := strings.TrimSpace(in.Filename)
	if name == "" || len(name) > 200 {
		name = "voice-note"
	}
	a, err := s.api().UploadAudio(ctx, ext(orgID), graphkit.AudioInput{Filename: name, ContentType: strings.ToLower(ct), Data: in.Data,
		DurationSeconds: in.DurationSeconds}, opKey("audio.upload"))
	if err != nil {
		return nil, s.upstream(ctx, "social.audio.upload", err)
	}
	return a, nil
}

// DeleteAudio removes a recording; GraphKit refuses while a queued step still sends it.
func (s *Service) DeleteAudio(ctx context.Context, orgID uuid.UUID, id string) error {
	if err := s.ready(ctx, orgID); err != nil {
		return err
	}
	if !strings.HasPrefix(id, "aud_") || len(id) > 100 {
		return errNotFound
	}
	if err := s.api().DeleteAudio(ctx, ext(orgID), id, opKey("audio.delete")); err != nil {
		return s.upstream(ctx, "social.audio.delete", err)
	}
	return nil
}
