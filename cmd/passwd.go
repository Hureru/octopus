package cmd

import (
	"fmt"

	"github.com/bestruirui/octopus/internal/conf"
	"github.com/bestruirui/octopus/internal/db"
	"github.com/bestruirui/octopus/internal/model"
	"github.com/spf13/cobra"
)

// passwdCmd 直接改库重置管理员密码，用于忘记密码时无法走需要旧密码的接口。
var passwdCmd = &cobra.Command{
	Use:   "passwd <password>",
	Short: "Set user password",
	Args:  cobra.ExactArgs(1),
	// 运行期错误（连不上库、没有用户）不是用法错误，不打印 usage。
	SilenceUsage: true,
	RunE: func(cmd *cobra.Command, args []string) error {
		if args[0] == "" {
			return fmt.Errorf("password must not be empty")
		}
		if err := conf.Load(cfgFile); err != nil {
			return err
		}
		if err := db.InitDB(conf.AppConfig.Database.Type, conf.AppConfig.Database.Path, conf.IsDebug()); err != nil {
			return fmt.Errorf("database init error: %w", err)
		}
		defer db.Close()

		var user model.User
		if err := db.GetDB().First(&user).Error; err != nil {
			return fmt.Errorf("failed to load user: %w", err)
		}
		user.Password = args[0]
		if err := user.HashPassword(); err != nil {
			return err
		}
		if err := db.GetDB().Model(&user).Update("password", user.Password).Error; err != nil {
			return fmt.Errorf("failed to update password: %w", err)
		}

		// 运行中的服务把用户缓存在内存里，不重启不会读到新密码。
		cmd.Println("Password updated. Restart the running server to apply the change.")
		return nil
	},
}

func init() {
	passwdCmd.Flags().StringVar(&cfgFile, "config", "", "config file (default is ./data/config.json)")
	rootCmd.AddCommand(passwdCmd)
}
