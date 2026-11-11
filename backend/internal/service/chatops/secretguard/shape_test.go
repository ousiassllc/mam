package secretguard_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/ousiassllc/mam/backend/internal/service/chatops/secretguard"
)

// A long name of a file of a medium and an ID the app made (UUIDv7) are no keys, alone or in a sentence, and not while the key card is open either.
func TestRedact_AFileNameAndAnIDOfTheAppAreNoKeys(t *testing.T) {
	for _, name := range []string{
		"scene01_shot02_take03_final_render.mp4",
		"final_cut_of_the_first_scene_v2.MOV",
		"IMG_20261110_123456_789012_345678.jpg",
		"A1B2C3D4E5F6G7H8I9J0K1L2.png",
		"0192f0c4-7a3e-7b1d-8c2f-5a6b7c8d9e0f",
	} {
		for _, text := range []string{name, "「" + name + "」", "これを使って " + name + " を書き出して", name + "."} {
			got, found := secretguard.Redact(text)
			assert.False(t, found, text)
			assert.Equal(t, text, got)
		}
		assert.False(t, secretguard.OneToken(name), name)
	}
}

// The whole message is a key only when it holds letters and digits, as a key in a sentence does: a long hyphenated phrase or a long word is not one.
func TestRedact_ALongWordWithoutDigitsIsNoKeyEvenWhenItIsTheWholeMessage(t *testing.T) {
	for _, text := range []string{"internationalization-and-localization", "state-of-the-art-model-name-here", "hello_world_this_is_a_long_word", "Supercalifragilisticexpialidocious"} {
		got, found := secretguard.Redact(text)
		assert.False(t, found, text)
		assert.Equal(t, text, got)
	}
}

// What stays a key: a UUID of any other version (Seedance and Flux keys), a key that only ends like a file, and a key that holds a UUIDv7 inside a longer token.
func TestRedact_TheKeysAroundTheExclusionsAreStillKeys(t *testing.T) {
	for _, key := range []string{
		"12345678-1234-4234-8234-123456789abc",
		"0192f0c4-7a3e-4b1d-8c2f-5a6b7c8d9e0f",
		"luma-0192f0c4-7a3e-7b1d-8c2f-5a6b7c8d9e0f",
		"a1b2c3d4e5f6g7h8i9j0k1l2m3n4.mp4x",
	} {
		got, found := secretguard.Redact(key)
		assert.True(t, found, key)
		assert.Equal(t, secretguard.Hidden, got)
	}
}
