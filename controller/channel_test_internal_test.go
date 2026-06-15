package controller

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/dto"
	"github.com/QuantumNous/new-api/pkg/billingexpr"
	relaycommon "github.com/QuantumNous/new-api/relay/common"
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

// TestShouldEnableAfterTest 覆盖探活后“是否恢复”的关键决策分支。
// 该决策是本次修复多 key 渠道无法自动恢复的核心：
//   - 多 key（forceKeyIndex>=0）：只要探测成功+开关开+有 key 即恢复（打通“部分 key 挂掉”场景）
//   - 单 key（forceKeyIndex<0）：仅渠道整体 AutoDisabled 且探测成功才恢复（保持原有行为）
func TestShouldEnableAfterTest(t *testing.T) {
	// 保存并恢复全局开关，避免污染其它测试
	orig := common.AutomaticEnableChannelEnabled
	t.Cleanup(func() { common.AutomaticEnableChannelEnabled = orig })

	someErr := types.NewOpenAIError(
		fmt.Errorf("boom"),
		types.ErrorCodeBadResponse,
		http.StatusInternalServerError,
	)

	cases := []struct {
		name           string
		enableSwitch   bool
		forceKeyIndex  int
		isChannelOn    bool
		channelStatus  int
		err            *types.NewAPIError
		usingKey       string
		want           bool
	}{
		// —— 总开关关闭：任何场景都不恢复 ——
		{
			name: "switch off blocks everything even multi-key success",
			enableSwitch: false, forceKeyIndex: 0, isChannelOn: true,
			channelStatus: common.ChannelStatusEnabled, usingKey: "k1",
			want: false,
		},

		// —— 多 key 路径（forceKeyIndex >= 0）——
		{
			name: "multi-key: disabled key probe success -> recover",
			enableSwitch: true, forceKeyIndex: 0, isChannelOn: true,
			channelStatus: common.ChannelStatusEnabled, usingKey: "k1",
			want: true,
		},
		{
			name: "multi-key: probe failed -> no recover",
			enableSwitch: true, forceKeyIndex: 1, isChannelOn: true,
			channelStatus: common.ChannelStatusEnabled, err: someErr, usingKey: "k2",
			want: false,
		},
		{
			name: "multi-key: empty usingKey -> no recover",
			enableSwitch: true, forceKeyIndex: 0, isChannelOn: true,
			channelStatus: common.ChannelStatusEnabled, usingKey: "",
			want: false,
		},
		// 关键：即使渠道整体仍是 Enabled（部分 key 挂掉），单 key 探测成功也要恢复。
		// 这是修复“死路 A”的核心断言——service.ShouldEnableChannel 在此场景会返回 false，
		// 故多 key 路径必须绕开它，本测试即锁定该行为。
		{
			name: "multi-key: recover individual key while channel still Enabled",
			enableSwitch: true, forceKeyIndex: 0, isChannelOn: true,
			channelStatus: common.ChannelStatusEnabled, usingKey: "k1",
			want: true,
		},
		// 渠道整体 AutoDisabled（所有 key 挂掉后），逐 key 探测成功也要恢复该 key（“死路 B”）。
		{
			name: "multi-key: recover key when channel AutoDisabled",
			enableSwitch: true, forceKeyIndex: 0, isChannelOn: false,
			channelStatus: common.ChannelStatusAutoDisabled, usingKey: "k1",
			want: true,
		},

		// —— 单 key 路径（forceKeyIndex < 0）——沿用原有 ShouldEnableChannel 语义 ——
		{
			name: "single-key: AutoDisabled + success -> recover",
			enableSwitch: true, forceKeyIndex: -1, isChannelOn: false,
			channelStatus: common.ChannelStatusAutoDisabled, usingKey: "only-key",
			want: true,
		},
		{
			name: "single-key: AutoDisabled but probe failed -> no recover",
			enableSwitch: true, forceKeyIndex: -1, isChannelOn: false,
			channelStatus: common.ChannelStatusAutoDisabled, err: someErr, usingKey: "only-key",
			want: false,
		},
		{
			name: "single-key: channel still enabled -> no recover (nothing to recover)",
			enableSwitch: true, forceKeyIndex: -1, isChannelOn: true,
			channelStatus: common.ChannelStatusEnabled, usingKey: "only-key",
			want: false,
		},
		{
			name: "single-key: ManuallyDisabled never auto-recovered",
			enableSwitch: true, forceKeyIndex: -1, isChannelOn: false,
			channelStatus: common.ChannelStatusManuallyDisabled, usingKey: "only-key",
			want: false,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			common.AutomaticEnableChannelEnabled = tc.enableSwitch
			got := shouldEnableAfterTest(tc.forceKeyIndex, tc.isChannelOn, tc.channelStatus, tc.err, tc.usingKey)
			require.Equal(t, tc.want, got)
		})
	}
}
