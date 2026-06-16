package model

import (
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/stretchr/testify/require"
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
