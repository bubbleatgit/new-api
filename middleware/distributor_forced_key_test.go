package middleware

import (
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"
	"github.com/QuantumNous/new-api/constant"
	"github.com/QuantumNous/new-api/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

// TestSetupContextForForcedKey 验证探活“强制使用指定 key”时，context 被正确写入。
// 这是修复多 key 渠道探活无法测到被禁用 key 的关键辅助函数：
// 当所有 key 都被禁用、GetNextEnabledKey 失败短路后，由它补做 context 设置。
func TestSetupContextForForcedKey(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("multi-key channel writes forced key and index", func(t *testing.T) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ch := &model.Channel{
			Key: "alpha\nbeta\ngamma",
			ChannelInfo: model.ChannelInfo{
				IsMultiKey: true,
			},
			Type: constant.ChannelTypeOpenAI,
		}

		SetupContextForForcedKey(ctx, ch, "beta", 1)

		require.Equal(t, "beta", common.GetContextKeyString(ctx, constant.ContextKeyChannelKey))
		require.True(t, common.GetContextKeyBool(ctx, constant.ContextKeyChannelIsMultiKey))
		require.Equal(t, 1, common.GetContextKeyInt(ctx, constant.ContextKeyChannelMultiKeyIndex))
		require.Equal(t, ch.GetBaseURL(), common.GetContextKeyString(ctx, constant.ContextKeyChannelBaseUrl))
		// 纯写入函数不应在任何情况下 panic；这里能读到即说明分支无短路
	})

	t.Run("single-key channel sets isMultiKey false", func(t *testing.T) {
		ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
		ch := &model.Channel{
			Key: "only-key",
			ChannelInfo: model.ChannelInfo{
				IsMultiKey: false,
			},
			Type: constant.ChannelTypeOpenAI,
		}

		SetupContextForForcedKey(ctx, ch, "only-key", 0)

		require.Equal(t, "only-key", common.GetContextKeyString(ctx, constant.ContextKeyChannelKey))
		require.False(t, common.GetContextKeyBool(ctx, constant.ContextKeyChannelIsMultiKey))
	})

	// 确保各 channel 类型分支不会 panic（覆盖 switch）。
	// 这里不逐一断言 c.Set 的值，仅保证不同 Type 都能正常执行 context 写入。
	t.Run("all channel type branches run without panic", func(t *testing.T) {
		types := []int{
			constant.ChannelTypeAzure,
			constant.ChannelTypeVertexAi,
			constant.ChannelTypeXunfei,
			constant.ChannelTypeGemini,
			constant.ChannelTypeAli,
			constant.ChannelCloudflare,
			constant.ChannelTypeMokaAI,
			constant.ChannelTypeCoze,
		}
		for _, ct := range types {
			ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
			ch := &model.Channel{
				Key:         "k",
				ChannelInfo: model.ChannelInfo{IsMultiKey: false},
				Type:        ct,
				Other:       "other-val",
			}
			require.NotPanics(t, func() {
				SetupContextForForcedKey(ctx, ch, "k", 0)
			})
		}
	})
}
