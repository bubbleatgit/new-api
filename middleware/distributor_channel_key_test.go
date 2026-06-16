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

func TestSetupContextForForcedChannelKeyWritesSelectedKeyContext(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ctx, _ := gin.CreateTestContext(httptest.NewRecorder())
	ch := &model.Channel{
		Key: "alpha\nbeta",
		ChannelInfo: model.ChannelInfo{
			IsMultiKey: true,
		},
		Type: constant.ChannelTypeOpenAI,
	}

	require.Nil(t, SetupContextForForcedChannelKey(ctx, ch, "gpt-4o-mini", "beta", 1))
	require.Equal(t, "beta", common.GetContextKeyString(ctx, constant.ContextKeyChannelKey))
	require.True(t, common.GetContextKeyBool(ctx, constant.ContextKeyChannelIsMultiKey))
	require.Equal(t, 1, common.GetContextKeyInt(ctx, constant.ContextKeyChannelMultiKeyIndex))
	require.Equal(t, "gpt-4o-mini", ctx.GetString("original_model"))
}
