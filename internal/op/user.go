package op

import (
	"fmt"
	"net/http"

	"github.com/bestruirui/octopus/internal/apperror"
	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/bestruirui/octopus/internal/utils/log"
)

var userCache model.User

func UserInit() error {
	if err := db.GetDB().First(&userCache).Error; err == nil {
		return nil
	}
	userCache.Username = "admin"
	userCache.Password = "admin"
	if err := userCache.HashPassword(); err != nil {
		return err
	}
	if err := db.GetDB().Create(&userCache).Error; err != nil {
		return err
	}
	log.Infof("initial user: admin,password: admin")
	return nil
}

func UserChangePassword(oldPassword, newPassword string) error {
	if err := userCache.ComparePassword(oldPassword); err != nil {
		return apperror.Wrap(apperror.CodeAuthPasswordIncorrect, "incorrect old password", err).WithStatus(http.StatusBadRequest)
	}

	userCache.Password = newPassword
	if err := userCache.HashPassword(); err != nil {
		return fmt.Errorf("failed to hash new password: %w", err)
	}

	if err := db.GetDB().Model(&userCache).Update("password", userCache.Password).Error; err != nil {
		return fmt.Errorf("failed to update password: %w", err)
	}

	return nil
}

func UserChangeUsername(newUsername string) error {
	if userCache.Username == newUsername {
		return fmt.Errorf("new username is the same as the old username")
	}
	userCache.Username = newUsername
	if err := db.GetDB().Model(&userCache).Update("username", userCache.Username).Error; err != nil {
		return fmt.Errorf("failed to update username: %w", err)
	}
	return nil
}

func UserVerify(username, password string) error {
	if username != userCache.Username {
		return fmt.Errorf("incorrect username")
	}
	if err := userCache.ComparePassword(password); err != nil {
		return fmt.Errorf("incorrect password")
	}
	return nil
}

func UserGet() model.User {
	return userCache
}
