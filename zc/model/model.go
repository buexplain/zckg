package model

import (
	"github.com/buexplain/zckg/zc/model/internal"
	"github.com/spf13/cobra"
)

var Cmd = &cobra.Command{
	Use:   "gen:model",
	Short: "生成表模型代码",
	Run: func(cmd *cobra.Command, args []string) {
		internal.Generate("D:/code/buexplain/zckg/zc/model/test", "test_db", "user_info", "db", "upperCamel", []internal.Column{
			{Name: "id", Type: "int", Comment: "主键"},
			{Name: "name", Type: "string", Comment: "用户名"},
			{Name: "PasswordMd5", Type: "string", Comment: "密码md5"},
			{Name: "user-Account", Type: "string", Comment: "用户账号另外一种格式"},
			{Name: "User_Account", Type: "string", Comment: "用户账号"},
			{Name: "UserAccount", Type: "string", Comment: "用户账号另外一种格式"},
		})
	},
}
