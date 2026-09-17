package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func insertMultiKeyChannelForStatusTest(t *testing.T, status int, statusList map[int]int) *Channel {
	t.Helper()
	autoBan := 1
	disabledReason := make(map[int]string)
	disabledTime := make(map[int]int64)
	for idx, keyStatus := range statusList {
		if keyStatus != common.ChannelStatusEnabled {
			disabledReason[idx] = "old error"
			disabledTime[idx] = int64(111 + idx)
		}
	}
	channel := &Channel{
		Type:    1,
		Key:     "key-a\nkey-b",
		Status:  status,
		Name:    "multi-key-status-test",
		Models:  "gpt-4o-mini",
		Group:   "default",
		AutoBan: &autoBan,
		ChannelInfo: ChannelInfo{
			IsMultiKey:             true,
			MultiKeySize:           2,
			MultiKeyStatusList:     statusList,
			MultiKeyDisabledReason: disabledReason,
			MultiKeyDisabledTime:   disabledTime,
		},
		OtherInfo: `{"status_reason":"All keys are disabled","status_time":111}`,
	}
	require.NoError(t, DB.Create(channel).Error)
	require.NoError(t, channel.AddAbilities(nil))
	return channel
}

func TestUpdateChannelStatusMultiKeyRecoversAutoDisabledKeyWhenChannelStillEnabled(t *testing.T) {
	truncateTables(t)
	origMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = origMemoryCache })

	channel := insertMultiKeyChannelForStatusTest(
		t,
		common.ChannelStatusEnabled,
		map[int]int{0: common.ChannelStatusAutoDisabled},
	)

	require.True(t, UpdateChannelStatus(channel.Id, "key-a", common.ChannelStatusEnabled, ""))

	got, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	require.Equal(t, common.ChannelStatusEnabled, got.Status)
	require.NotContains(t, got.ChannelInfo.MultiKeyStatusList, 0)
	require.NotContains(t, got.ChannelInfo.MultiKeyDisabledReason, 0)
	require.NotContains(t, got.ChannelInfo.MultiKeyDisabledTime, 0)
	require.Len(t, got.ChannelInfo.MultiKeyStatusList, 0)
}

func TestUpdateChannelStatusMultiKeyRecoversChannelWhenAnAutoDisabledKeyWorks(t *testing.T) {
	truncateTables(t)
	origMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = origMemoryCache })

	channel := insertMultiKeyChannelForStatusTest(
		t,
		common.ChannelStatusAutoDisabled,
		map[int]int{0: common.ChannelStatusAutoDisabled, 1: common.ChannelStatusAutoDisabled},
	)

	require.True(t, UpdateChannelStatus(channel.Id, "key-a", common.ChannelStatusEnabled, ""))

	got, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	require.Equal(t, common.ChannelStatusEnabled, got.Status)
	require.NotContains(t, got.ChannelInfo.MultiKeyStatusList, 0)
	require.Contains(t, got.ChannelInfo.MultiKeyStatusList, 1)
	require.NotContains(t, got.ChannelInfo.MultiKeyDisabledReason, 0)
	require.NotContains(t, got.ChannelInfo.MultiKeyDisabledTime, 0)
	require.NotContains(t, got.GetOtherInfo(), "status_reason")
	require.NotContains(t, got.GetOtherInfo(), "status_time")

	var ability Ability
	require.NoError(t, DB.Where("channel_id = ?", channel.Id).First(&ability).Error)
	require.True(t, ability.Enabled)
}

func TestUpdateChannelStatusMultiKeyDoesNotRecoverManuallyDisabledKey(t *testing.T) {
	truncateTables(t)
	origMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() { common.MemoryCacheEnabled = origMemoryCache })

	channel := insertMultiKeyChannelForStatusTest(
		t,
		common.ChannelStatusEnabled,
		map[int]int{0: common.ChannelStatusManuallyDisabled},
	)

	require.False(t, UpdateChannelStatus(channel.Id, "key-a", common.ChannelStatusEnabled, ""))

	got, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)
	require.Equal(t, common.ChannelStatusEnabled, got.Status)
	require.Equal(t, common.ChannelStatusManuallyDisabled, got.ChannelInfo.MultiKeyStatusList[0])
	require.Contains(t, got.ChannelInfo.MultiKeyDisabledReason, 0)
	require.Contains(t, got.ChannelInfo.MultiKeyDisabledTime, 0)
}

func TestUpdateChannelStatusMultiKeyRecoveryRestoresMemoryCacheIndex(t *testing.T) {
	truncateTables(t)
	origMemoryCache := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = true
	t.Cleanup(func() {
		common.MemoryCacheEnabled = origMemoryCache
		channelSyncLock.Lock()
		group2model2channels = nil
		channelsIDM = nil
		channelSyncLock.Unlock()
	})

	channel := insertMultiKeyChannelForStatusTest(
		t,
		common.ChannelStatusAutoDisabled,
		map[int]int{0: common.ChannelStatusAutoDisabled, 1: common.ChannelStatusAutoDisabled},
	)
	InitChannelCache()
	require.False(t, IsChannelEnabledForGroupModel("default", "gpt-4o-mini", channel.Id))

	require.True(t, UpdateChannelStatus(channel.Id, "key-a", common.ChannelStatusEnabled, ""))

	cached, err := CacheGetChannel(channel.Id)
	require.NoError(t, err)
	require.Equal(t, common.ChannelStatusEnabled, cached.Status)
	require.True(t, IsChannelEnabledForGroupModel("default", "gpt-4o-mini", channel.Id))
}

func setupChannelStatusTest(t *testing.T) {
	t.Helper()
	truncateTables(t)
	require.NoError(t, DB.Exec("DELETE FROM abilities").Error)
	require.NoError(t, DB.Exec("DELETE FROM channels").Error)

	memoryCacheEnabled := common.MemoryCacheEnabled
	common.MemoryCacheEnabled = false
	t.Cleanup(func() {
		common.MemoryCacheEnabled = memoryCacheEnabled
	})
}

func TestUpdateChannelStatusPersistsMultiKeyState(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:   "multi-key-status",
		Key:    "key-a\nkey-b",
		Status: common.ChannelStatusEnabled,
		ChannelInfo: ChannelInfo{
			IsMultiKey:           true,
			MultiKeySize:         2,
			MultiKeyMode:         constant.MultiKeyModePolling,
			MultiKeyPollingIndex: 1,
		},
	}
	require.NoError(t, DB.Create(&channel).Error)

	changed := UpdateChannelStatus(channel.Id, "key-a", common.ChannelStatusAutoDisabled, "provider rejected key")
	require.True(t, changed)

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusEnabled, stored.Status)
	assert.Equal(t, common.ChannelStatusAutoDisabled, stored.ChannelInfo.MultiKeyStatusList[0])
	assert.Equal(t, "provider rejected key", stored.ChannelInfo.MultiKeyDisabledReason[0])
	assert.NotZero(t, stored.ChannelInfo.MultiKeyDisabledTime[0])
	assert.Equal(t, 1, stored.ChannelInfo.MultiKeyPollingIndex)
}

func TestSaveStatusStateFromSingleKeySnapshotPreservesUnownedColumns(t *testing.T) {
	setupChannelStatusTest(t)

	channel := Channel{
		Name:        "single-key-status",
		Key:         "original-key",
		Status:      common.ChannelStatusEnabled,
		Models:      "original-model",
		Group:       "default",
		UsedQuota:   100,
		ChannelInfo: ChannelInfo{},
	}
	require.NoError(t, DB.Create(&channel).Error)

	stale, err := GetChannelById(channel.Id, true)
	require.NoError(t, err)

	concurrentChannelInfo := ChannelInfo{
		IsMultiKey:           true,
		MultiKeySize:         2,
		MultiKeyMode:         constant.MultiKeyModePolling,
		MultiKeyPollingIndex: 1,
	}
	require.NoError(t, DB.Model(&Channel{}).Where("id = ?", channel.Id).Updates(map[string]any{
		"key":          "rotated-key",
		"used_quota":   gorm.Expr("used_quota + ?", 250),
		"models":       "concurrent-model",
		"channel_info": concurrentChannelInfo,
	}).Error)

	stale.Status = common.ChannelStatusManuallyDisabled
	stale.SetOtherInfo(map[string]any{
		"status_reason": "manual operation",
		"status_time":   int64(1234),
	})
	require.NoError(t, stale.saveStatusState())

	var stored Channel
	require.NoError(t, DB.First(&stored, channel.Id).Error)
	assert.Equal(t, common.ChannelStatusManuallyDisabled, stored.Status)
	assert.Equal(t, "rotated-key", stored.Key)
	assert.Equal(t, int64(350), stored.UsedQuota)
	assert.Equal(t, "concurrent-model", stored.Models)
	assert.Equal(t, concurrentChannelInfo, stored.ChannelInfo)

	otherInfo := stored.GetOtherInfo()
	assert.Equal(t, "manual operation", otherInfo["status_reason"])
	assert.Equal(t, float64(1234), otherInfo["status_time"])
}
