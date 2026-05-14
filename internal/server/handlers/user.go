package handlers

import (
	"net/http"

	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/op"
	"github.com/bestruirui/octopus/internal/server/auth"
	"github.com/bestruirui/octopus/internal/server/middleware"
	"github.com/bestruirui/octopus/internal/server/resp"
	"github.com/bestruirui/octopus/internal/server/router"
	"github.com/gin-gonic/gin"
)

func init() {
	router.NewGroupRouter("/api/v1/user").
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/login", http.MethodPost).
				Handle(login),
		)
	router.NewGroupRouter("/api/v1/user").
		Use(middleware.Auth()).
		Use(middleware.RequireJSON()).
		AddRoute(
			router.NewRoute("/change-password", http.MethodPost).
				Handle(changePassword),
		).
		AddRoute(
			router.NewRoute("/change-username", http.MethodPost).
				Handle(changeUsername),
		).
		AddRoute(
			router.NewRoute("/status", http.MethodGet).
				Handle(status),
		)
}

func login(c *gin.Context) {
	var user model.UserLogin
	if err := c.ShouldBindJSON(&user); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := op.UserVerify(user.Username, user.Password); err != nil {
		resp.InvalidCredentials(c)
		return
	}
	token, expire, err := auth.GenerateJWTToken(user.Expire)
	if err != nil {
		resp.InternalError(c)
		return
	}
	resp.Success(c, model.UserLoginResponse{Token: token, ExpireAt: expire})
}

func changePassword(c *gin.Context) {
	var user model.UserChangePassword
	if err := c.ShouldBindJSON(&user); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := op.UserChangePassword(user.OldPassword, user.NewPassword); err != nil {
		resp.ErrorWithAppError(c, http.StatusInternalServerError, err)
		return
	}
	resp.Success(c, "password changed successfully")
}

func changeUsername(c *gin.Context) {
	var user model.UserChangeUsername
	if err := c.ShouldBindJSON(&user); err != nil {
		resp.InvalidJSON(c)
		return
	}
	if err := op.UserChangeUsername(user.NewUsername); err != nil {
		resp.Error(c, http.StatusInternalServerError, err.Error())
		return
	}
	resp.Success(c, "username changed successfully")
}

func status(c *gin.Context) {
	resp.Success(c, "ok")
}
