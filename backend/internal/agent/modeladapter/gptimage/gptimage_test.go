package gptimage_test

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ousiassllc/mam/backend/internal/agent/modeladapter/gptimage"
	"github.com/ousiassllc/mam/backend/internal/domain"
)

func TestEstimateIsThePriceOfTheProfileAtTheSize(t *testing.T) {
	assert.InDelta(t, 0.041, gptimage.Estimate("medium", domain.AspectRatio16x9), 1e-9)
	assert.InDelta(t, 0.041, gptimage.Estimate("medium", domain.AspectRatio9x16), 1e-9)
	assert.InDelta(t, 0.053, gptimage.Estimate("medium", domain.AspectRatio1x1), 1e-9)
	assert.InDelta(t, 0.005, gptimage.Estimate("low", domain.AspectRatio16x9), 1e-9)
	assert.InDelta(t, 0.165, gptimage.Estimate("high", domain.AspectRatio16x9), 1e-9)
}

func TestEstimateWithoutASizeIsTheDearestOne(t *testing.T) {
	for _, q := range gptimage.Qualities() {
		for _, r := range domain.BriefAspectRatios() {
			assert.GreaterOrEqual(t, gptimage.Estimate(string(q), ""), gptimage.Estimate(string(q), r), "%s at %s", q, r)
		}
	}
	assert.InDelta(t, 0.211, gptimage.Estimate("high", ""), 1e-9)
}

func TestEveryProfileIsPricedAtEverySizeBriefsUse(t *testing.T) {
	for _, q := range gptimage.Qualities() {
		for _, r := range domain.BriefAspectRatios() {
			assert.Positive(t, gptimage.Estimate(string(q), r), "%s at %s", q, r)
		}
	}
}

func TestAnEmptySettingIsTheDefaultProfile(t *testing.T) {
	assert.Equal(t, gptimage.Default, gptimage.Effective(""))
	assert.Equal(t, gptimage.Estimate(string(gptimage.Default), "16:9"), gptimage.Estimate("", "16:9"))
	assert.True(t, gptimage.Valid(""))
	assert.True(t, gptimage.Valid("high"))
	assert.False(t, gptimage.Valid("auto"), "auto leaves the price to the model")
	assert.False(t, gptimage.Valid("ultra"))
}

func TestActualCostIsTheTokensAtTheTablePrices(t *testing.T) {
	u := &gptimage.Usage{InputTokens: 1300, OutputTokens: 1368}
	u.InputDetails.TextTokens, u.InputDetails.ImageTokens = 300, 1000

	got := gptimage.ActualCost("gpt-image-2", u)

	require.NotNil(t, got)
	// 300 text × $5 + 1000 image × $8 + 1368 output × $30, per million tokens
	assert.InDelta(t, (300*5.0+1000*8.0+1368*30.0)/1e6, *got, 1e-12)
}

func TestActualCostCountsInputTheDetailsLeaveOutAsText(t *testing.T) {
	got := gptimage.ActualCost("gpt-image-2", &gptimage.Usage{InputTokens: 200, OutputTokens: 100})

	require.NotNil(t, got)
	assert.InDelta(t, (200*5.0+100*30.0)/1e6, *got, 1e-12)
}

func TestActualCostIsUnknownWithoutUsageOrForAnotherModel(t *testing.T) {
	assert.Nil(t, gptimage.ActualCost("gpt-image-2", nil), "the response had no usage")
	assert.Nil(t, gptimage.ActualCost("gpt-image-1.5", &gptimage.Usage{OutputTokens: 100}), "other models have other prices")
	assert.NotNil(t, gptimage.ActualCost("gpt-image-2-2026-04-21", &gptimage.Usage{OutputTokens: 100}), "a snapshot of the model")
}

func TestActualCostOfACountThatCannotBeRealIsUnknown(t *testing.T) {
	huge := math.MaxInt64
	usage := func(in, text, image, out int) *gptimage.Usage {
		u := &gptimage.Usage{InputTokens: in, OutputTokens: out}
		u.InputDetails.TextTokens, u.InputDetails.ImageTokens = text, image
		return u
	}
	for name, tc := range map[string]struct {
		usage  *gptimage.Usage
		usable bool
	}{
		"the largest image of mam":          {usage(100, 100, 0, 7033), true},
		"the ceiling of output":             {usage(0, 0, 0, 100_000), true},
		"over the ceiling of output":        {usage(0, 0, 0, 100_001), false},
		"a huge output":                     {usage(0, 0, 0, 9_000_000_000_000_000_000), false},
		"a negative output":                 {usage(0, 0, 0, -1), false},
		"the ceiling of input":              {usage(200_000, 0, 200_000, 100), true},
		"over the ceiling of input":         {usage(200_001, 0, 0, 100), false},
		"a negative input":                  {usage(-5, 0, 0, 100), false},
		"a huge text count":                 {usage(100, huge, 0, 100), false},
		"a negative image count":            {usage(100, 0, -1, 100), false},
		"details that wrap around":          {usage(100, huge, huge, 100), false},
		"a total that is below the details": {usage(10, 300, 1000, 100), true},
	} {
		t.Run(name, func(t *testing.T) {
			got := gptimage.ActualCost("gpt-image-2", tc.usage)

			if tc.usable {
				require.NotNil(t, got)
				assert.GreaterOrEqual(t, *got, 0.0)
				return
			}
			assert.Nil(t, got, "the estimate is used")
		})
	}
}

func TestParseUsageNeverFails(t *testing.T) {
	for name, tc := range map[string]struct {
		raw  string
		want *gptimage.Usage
	}{
		"absent":              {"", nil},
		"null":                {"null", nil},
		"not an object":       {`"x"`, nil},
		"a string count":      {`{"output_tokens":"x"}`, nil},
		"a fractional count":  {`{"output_tokens":1300.5}`, nil},
		"over int64":          {`{"output_tokens":99999999999999999999}`, nil},
		"a string detail":     {`{"input_tokens_details":{"text_tokens":"3"}}`, nil},
		"a well-formed usage": {`{"input_tokens":68,"output_tokens":1300}`, &gptimage.Usage{InputTokens: 68, OutputTokens: 1300}},
	} {
		t.Run(name, func(t *testing.T) {
			assert.Equal(t, tc.want, gptimage.ParseUsage([]byte(tc.raw)))
		})
	}
}
