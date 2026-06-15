package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/model"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
	"github.com/QuantumNous/new-api/setting/operation_setting"
	"github.com/QuantumNous/new-api/types"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestSettleTestQuotaUsesTieredBilling(t *testing.T) {
	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode:   "tiered_expr",
			ExprString:    `param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`,
			ExprHash:      billingexpr.ExprHashString(`param("stream") == true ? tier("stream", p * 3) : tier("base", p * 2)`),
			GroupRatio:    1,
			EstimatedTier: "stream",
			QuotaPerUnit:  common.QuotaPerUnit,
			ExprVersion:   1,
		},
		BillingRequestInput: &billingexpr.RequestInput{
			Body: []byte(`{"stream":true}`),
		},
	}

	quota, result := settleTestQuota(info, types.PriceData{
		ModelRatio:      1,
		CompletionRatio: 2,
	}, &dto.Usage{
		PromptTokens: 1000,
	})

	require.Equal(t, 1500, quota)
	require.NotNil(t, result)
	require.Equal(t, "stream", result.MatchedTier)
}

func TestBuildTestLogOtherInjectsTieredInfo(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())

	info := &relaycommon.RelayInfo{
		TieredBillingSnapshot: &billingexpr.BillingSnapshot{
			BillingMode: "tiered_expr",
			ExprString:  `tier("base", p * 2)`,
		},
		ChannelMeta: &relaycommon.ChannelMeta{},
	}
	priceData := types.PriceData{
		GroupRatioInfo: types.GroupRatioInfo{GroupRatio: 1},
	}
	usage := &dto.Usage{
		PromptTokensDetails: dto.InputTokenDetails{
			CachedTokens: 12,
		},
	}

	other := buildTestLogOther(ctx, info, priceData, usage, &billingexpr.TieredResult{
		MatchedTier: "base",
	})

	require.Equal(t, "tiered_expr", other["billing_mode"])
	require.Equal(t, "base", other["matched_tier"])
	require.NotEmpty(t, other["expr_b64"])
}

func TestResolveChannelTestUserIDUsesRequestUser(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ctx.Set("id", 2)

	userID, err := resolveChannelTestUserID(ctx)

	require.NoError(t, err)
	require.Equal(t, 2, userID)
}

func TestShouldProbeMultiKey(t *testing.T) {
	cases := []struct {
		name       string
		statusList map[int]int
		keyIndex   int
		want       bool
	}{
		{
			name:       "nil status list -> probe",
			statusList: nil,
			keyIndex:   0,
			want:       true,
		},
		{
			name:       "key not in list -> probe",
			statusList: map[int]int{1: common.ChannelStatusAutoDisabled},
			keyIndex:   0,
			want:       true,
		},
		{
			name:       "manual disabled key -> skip",
			statusList: map[int]int{0: common.ChannelStatusManuallyDisabled},
			keyIndex:   0,
			want:       false,
		},
		{
			name:       "auto disabled key -> probe",
			statusList: map[int]int{0: common.ChannelStatusAutoDisabled},
			keyIndex:   0,
			want:       true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ch := &model.Channel{
				ChannelInfo: model.ChannelInfo{
					IsMultiKey:         true,
					MultiKeyStatusList: tc.statusList,
				},
			}
			require.Equal(t, tc.want, shouldProbeMultiKey(ch, tc.keyIndex))
		})
	}
}

func TestShouldEnableAfterTest(t *testing.T) {
	orig := common.AutomaticEnableChannelEnabled
	t.Cleanup(func() { common.AutomaticEnableChannelEnabled = orig })

	someErr := types.NewOpenAIError(
		fmt.Errorf("boom"),
		types.ErrorCodeBadResponse,
		http.StatusInternalServerError,
	)

	cases := []struct {
		name          string
		enableSwitch  bool
		forceKeyIndex int
		isChannelOn   bool
		channelStatus int
		keyStatusList map[int]int
		err           *types.NewAPIError
		usingKey      string
		want          bool
	}{
		{
			name:          "switch off blocks multi-key success",
			enableSwitch:  false,
			forceKeyIndex: 0,
			isChannelOn:   true,
			channelStatus: common.ChannelStatusEnabled,
			keyStatusList: map[int]int{0: common.ChannelStatusAutoDisabled},
			usingKey:      "k1",
			want:          false,
		},
		{
			name:          "multi-key auto disabled key success -> recover",
			enableSwitch:  true,
			forceKeyIndex: 0,
			isChannelOn:   true,
			channelStatus: common.ChannelStatusEnabled,
			keyStatusList: map[int]int{0: common.ChannelStatusAutoDisabled},
			usingKey:      "k1",
			want:          true,
		},
		{
			name:          "multi-key probe failed -> no recover",
			enableSwitch:  true,
			forceKeyIndex: 1,
			isChannelOn:   true,
			channelStatus: common.ChannelStatusEnabled,
			keyStatusList: map[int]int{1: common.ChannelStatusAutoDisabled},
			err:           someErr,
			usingKey:      "k2",
			want:          false,
		},
		{
			name:          "multi-key enabled key success -> no recover notification",
			enableSwitch:  true,
			forceKeyIndex: 0,
			isChannelOn:   true,
			channelStatus: common.ChannelStatusEnabled,
			keyStatusList: nil,
			usingKey:      "k1",
			want:          false,
		},
		{
			name:          "multi-key manual disabled key success -> no recover",
			enableSwitch:  true,
			forceKeyIndex: 0,
			isChannelOn:   true,
			channelStatus: common.ChannelStatusEnabled,
			keyStatusList: map[int]int{0: common.ChannelStatusManuallyDisabled},
			usingKey:      "k1",
			want:          false,
		},
		{
			name:          "single-key auto disabled success -> recover",
			enableSwitch:  true,
			forceKeyIndex: -1,
			isChannelOn:   false,
			channelStatus: common.ChannelStatusAutoDisabled,
			usingKey:      "only-key",
			want:          true,
		},
		{
			name:          "single-key manual disabled never recovers",
			enableSwitch:  true,
			forceKeyIndex: -1,
			isChannelOn:   false,
			channelStatus: common.ChannelStatusManuallyDisabled,
			usingKey:      "only-key",
			want:          false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			common.AutomaticEnableChannelEnabled = tc.enableSwitch
			channel := &model.Channel{
				ChannelInfo: model.ChannelInfo{
					IsMultiKey:         tc.forceKeyIndex >= 0,
					MultiKeyStatusList: tc.keyStatusList,
				},
			}
			got := shouldEnableAfterTest(channel, tc.forceKeyIndex, tc.isChannelOn, tc.channelStatus, tc.err, tc.usingKey)
			require.Equal(t, tc.want, got)
		})
	}
}

func TestSelectChannelsForAutomaticTestPassiveRecoveryOnlyUsesAutoDisabled(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModePassiveRecovery)

	require.Len(t, selected, 1)
	require.Equal(t, 2, selected[0].Id)
}

func TestSelectChannelsForAutomaticTestScheduledSkipsManualDisabled(t *testing.T) {
	channels := []*model.Channel{
		{Id: 1, Status: common.ChannelStatusEnabled},
		{Id: 2, Status: common.ChannelStatusAutoDisabled},
		{Id: 3, Status: common.ChannelStatusManuallyDisabled},
	}

	selected := selectChannelsForAutomaticTest(channels, operation_setting.ChannelTestModeScheduledAll)

	require.Len(t, selected, 2)
	require.Equal(t, 1, selected[0].Id)
	require.Equal(t, 2, selected[1].Id)
}

func TestTestAllChannelsRejectsExistingActiveTask(t *testing.T) {
	db := setupModelListControllerTestDB(t)
	require.NoError(t, db.AutoMigrate(&model.SystemTask{}, &model.SystemTaskLock{}))

	existing, err := model.CreateSystemTask(model.SystemTaskTypeChannelTest, nil, nil)
	require.NoError(t, err)

	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/api/channel/test", nil)

	TestAllChannels(ctx)

	require.Equal(t, http.StatusConflict, recorder.Code)
	require.Contains(t, recorder.Body.String(), existing.TaskID)
	require.Contains(t, recorder.Body.String(), "已有通道测试任务正在运行或等待中")
}
