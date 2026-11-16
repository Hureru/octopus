package sitesync

import (
	"context"

	"github.com/bestruirui/octopus/internal/model"
)

// AgentRouter 是 new-api 二开，同步走 New API 流程，只有签到不同：站点没有签到接口
// （/api/user/checkin、/api/user/sign_in 都是 404），额度是登录时发的，登录响应的
// data.checked_in 表示这次登录有没有新增额度。所以签到 = 用账号密码重新登录一次；
// Access Token 只能调接口、触发不了登录，这类账号要额外填用户名密码才能签到。
func checkinAgentRouter(ctx context.Context, siteRecord *model.Site, account *model.SiteAccount) (*model.SiteCheckinResult, string, error) {
	if account.Username == "" || account.Password == "" {
		return &model.SiteCheckinResult{
			Status:  model.SiteExecutionStatusSkipped,
			Message: "AgentRouter 靠重新登录触发签到，请在账号里填写用户名和密码",
		}, "", nil
	}

	// 登录成功（payload 非 nil）即已签到，拿不到 session cookie 也不影响签到结果
	payload, sessionCookie, err := anyRouterLogin(ctx, siteRecord, account)
	if payload == nil {
		return nil, "", err
	}
	if account.CredentialType != model.SiteCredentialTypeUsernamePassword {
		// 这类账号只借登录来签到：返回 session cookie 会覆盖掉已存的系统访问令牌
		sessionCookie = ""
	}

	message := "登录成功，本次没有新增额度（已签到过）"
	if jsonBool(nestedValue(payload, "data", "checked_in")) {
		message = "签到成功，新增额度已到账"
	}
	return &model.SiteCheckinResult{Status: model.SiteExecutionStatusSuccess, Message: message}, sessionCookie, nil
}
