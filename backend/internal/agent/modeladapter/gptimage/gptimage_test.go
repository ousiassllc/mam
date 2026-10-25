package gptimage_test

import (
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
