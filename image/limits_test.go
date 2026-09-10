package image

import (
	"testing"

	"github.com/dandandujie/dsr-go/core"
)

func TestReservingBytesReducesTheAvailableBytes(t *testing.T) {
	budget := NewImageByteBudget(8, 10, 0)
	requireEqual(t, budget.MaxImageBytes(), 8, "MaxImageBytes()")
	requireNoError(t, budget.Reserve(6))

	err := budget.Reserve(5)
	imageErr := requireImageError(t, err, KindTotalSizeTooLarge)
	requireEqual(t, imageErr.Size, 11, "Size")
	requireEqual(t, imageErr.Max, 10, "Max")
	requireNoError(t, budget.Reserve(4))
}

func TestABudgetAccountsForTheBytesOfEarlierCalls(t *testing.T) {
	budget := NewImageByteBudget(8, 10, 6)

	err := budget.Reserve(5)
	imageErr := requireImageError(t, err, KindTotalSizeTooLarge)
	requireEqual(t, imageErr.Size, 11, "Size")
	requireEqual(t, imageErr.Max, 10, "Max")
	requireNoError(t, budget.Reserve(4))
}

func TestAReleaseReturnsTheBytesOfAFailedAttempt(t *testing.T) {
	budget := NewImageByteBudget(8, 10, 0)
	requireNoError(t, budget.Reserve(10))
	budget.Release(10)
	requireNoError(t, budget.Reserve(10))
}

func TestDefaultImageLimitsMatchTheRustDefaults(t *testing.T) {
	limits := DefaultImageLimits()
	requireEqual(t, limits.MaxImages, 600, "MaxImages")
	requireEqual(t, limits.MaxURLLen, 8192, "MaxURLLen")
	requireEqual(t, limits.MaxImageBytes, 32*1024*1024, "MaxImageBytes")
	requireEqual(t, limits.MaxTotalBytes, 64*1024*1024, "MaxTotalBytes")
	requireEqual(t, limits.MaxConcurrentSources, 8, "MaxConcurrentSources")
	requireEqual(t, limits.MaxDimensionPx, uint32(8192), "MaxDimensionPx")
	requireEqual(t, limits.MaxDimensionOnManyImagesPx, uint32(4096), "MaxDimensionOnManyImagesPx")
	requireEqual(t, limits.ManyImagesThreshold, 15, "ManyImagesThreshold")
	requireEqual(t, limits.LowDetailMaxDimensionPx, uint32(512), "LowDetailMaxDimensionPx")
}

func TestMaxDimensionSwitchesAtTheManyImagesThreshold(t *testing.T) {
	limits := DefaultImageLimits()
	requireEqual(t, limits.MaxDimension(14), uint32(8192), "MaxDimension(14)")
	requireEqual(t, limits.MaxDimension(15), uint32(4096), "MaxDimension(15)")
	options := limits.PreprocessOptions(core.ImageDetailLow, 15)
	requireEqual(t, options.Detail, core.ImageDetailLow, "Detail")
	requireEqual(t, options.MaxDimensionPx, uint32(4096), "MaxDimensionPx")
	requireEqual(t, options.LowDetailMaxDimensionPx, uint32(512), "LowDetailMaxDimensionPx")
}

func TestImageQuotaRecordsOnlyCompletedCalls(t *testing.T) {
	quota := NewImageQuota()
	requireEqual(t, quota.ImageCount(), 0, "ImageCount()")
	requireEqual(t, quota.ByteSize(), 0, "ByteSize()")
	quota.add(2, 12)
	requireEqual(t, quota.ImageCount(), 2, "ImageCount()")
	requireEqual(t, quota.ByteSize(), 12, "ByteSize()")
}
