package test

// UserInfoEntity test_db.user_info 表 entity 结构体，常用于数据库读取操作。
type UserInfoEntity struct {
	ID int `json:"Id" db:"id" description:"主键"`
	Name string `json:"Name" db:"name" description:"用户名"`
	PasswordMd5 string `json:"PasswordMd5" db:"PasswordMd5" description:"密码md5"`
	User_Account string `json:"User_Account" db:"user-Account" description:"用户账号另外一种格式"`
	UseRAccount string `json:"UseRAccount" db:"User_Account" description:"用户账号"`
	UserAccount string `json:"UserAccount" db:"UserAccount" description:"用户账号另外一种格式"`
}

func (e *UserInfoEntity) ToDO(userInfoDO ...*UserInfoDO) *UserInfoDO {
	var d *UserInfoDO
	if len(userInfoDO) > 0 && userInfoDO[0] != nil {
		d = userInfoDO[0]
	} else {
		d = &UserInfoDO{}
	}
	d.ID = &e.ID
	d.Name = &e.Name
	d.PasswordMd5 = &e.PasswordMd5
	d.User_Account = &e.User_Account
	d.UseRAccount = &e.UseRAccount
	d.UserAccount = &e.UserAccount
	return d
}

// UserInfoDO test_db.user_info 表 do 结构体，常用于数据库写入操作。
type UserInfoDO struct {
	ID *int `json:"Id" db:"id" description:"主键"`
	Name *string `json:"Name" db:"name" description:"用户名"`
	PasswordMd5 *string `json:"PasswordMd5" db:"PasswordMd5" description:"密码md5"`
	User_Account *string `json:"User_Account" db:"user-Account" description:"用户账号另外一种格式"`
	UseRAccount *string `json:"UseRAccount" db:"User_Account" description:"用户账号"`
	UserAccount *string `json:"UserAccount" db:"UserAccount" description:"用户账号另外一种格式"`
}

func (d *UserInfoDO) ToEntity(userInfoEntity ...*UserInfoEntity) *UserInfoEntity {
	var e *UserInfoEntity
	if len(userInfoEntity) > 0 && userInfoEntity[0] != nil {
		e = userInfoEntity[0]
	} else {
		e = &UserInfoEntity{}
	}
	if d.ID != nil {
		e.ID = *d.ID
	}
	if d.Name != nil {
		e.Name = *d.Name
	}
	if d.PasswordMd5 != nil {
		e.PasswordMd5 = *d.PasswordMd5
	}
	if d.User_Account != nil {
		e.User_Account = *d.User_Account
	}
	if d.UseRAccount != nil {
		e.UseRAccount = *d.UseRAccount
	}
	if d.UserAccount != nil {
		e.UserAccount = *d.UserAccount
	}
	return e
}
