// Package shotaudio is the audio asset a Shot is made from (Issue #1153): the "audio" key of shot.shot_spec, {"audio": {"asset_id": "<asset ID>"}}. A speech-to-video model
// (Wan 2.2 S2V) makes the Shot's clip from that sound and one reference image. The key is owned here: the Canonical Shot Spec (domain.MergeShotSpec), the Reference Pack
// (domain.WithReferences) and the storyboard keep it as they keep every key they do not own, and a row written before the key existed has none (the Shot has no audio).
// It is a package of its own because domain is at its size limit.
package shotaudio

import (
	"encoding/json"
	"fmt"
)

// key is the shot_spec key.
const key = "audio"

// ExcludedNoAudio is the reason (domain.RoutingCandidate.Excluded) Multi-model routing leaves a model out of the weighing for a Shot with audio: it takes no sound file.
const ExcludedNoAudio = "no_audio_input"

type tie struct {
	AssetID string `json:"asset_id"`
}

// ID is the asset ID the Shot's shot_spec ties the Shot to; "" when the Shot has no audio (no key, an empty ID, or a shot_spec that is not a JSON object: a row of an older
// version reads as a Shot without audio, never as an error).
func ID(shotSpec json.RawMessage) string {
	var doc struct {
		Audio *tie `json:"audio"`
	}
	if err := json.Unmarshal(shotSpec, &doc); err != nil || doc.Audio == nil {
		return ""
	}
	return doc.Audio.AssetID
}

// With is shotSpec with the audio set to the asset; an empty assetID removes the key. Every other key stays as it is. A shotSpec that is not a JSON object is an error
// (nothing is overwritten); an empty one is "{}".
func With(shotSpec json.RawMessage, assetID string) (json.RawMessage, error) {
	doc := map[string]json.RawMessage{}
	if len(shotSpec) > 0 {
		if err := json.Unmarshal(shotSpec, &doc); err != nil {
			return nil, fmt.Errorf("shot_spec is not a JSON object: %w", err)
		}
		if doc == nil { // the JSON null
			doc = map[string]json.RawMessage{}
		}
	}
	if assetID == "" {
		delete(doc, key)
	} else {
		raw, err := json.Marshal(tie{AssetID: assetID})
		if err != nil { // only a string: Marshal cannot fail
			return nil, err
		}
		doc[key] = raw
	}
	return json.Marshal(doc)
}
