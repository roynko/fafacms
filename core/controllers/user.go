package controllers

import (
	"fmt"
	"github.com/gin-gonic/gin"
	"github.com/hunterhug/fafacms/core/config"
	"github.com/hunterhug/fafacms/core/flog"
	"github.com/hunterhug/fafacms/core/model"
	"github.com/hunterhug/fafacms/core/util/mail"
	"github.com/hunterhug/parrot/util"
	"time"
)

type RegisterUserRequest struct {
	Name      string `json:"name" validate:"required,alphanumunicode,gt=1,lt=50"`
	NickName  string `json:"nick_name" validate:"required,gt=1,lt=50"`
	Email     string `json:"email" validate:"required,email"`
	WeChat    string `json:"wechat" validate:"omitempty,alphanumunicode,gt=3,lt=30"`
	WeiBo     string `json:"weibo" validate:"omitempty,url"`
	Github    string `json:"github" validate:"omitempty,url"`
	QQ        string `json:"qq" validate:"omitempty,numeric,gt=6,lt=12"`
	Password  string `json:"password" validate:"alphanumunicode,gt=5,lt=17"`
	Gender    int    `json:"gender" validate:"oneof=0 1 2"`
	Describe  string `json:"describe" validate:"omitempty,lt=200"`
	ImagePath string `json:"image_path" validate:"omitempty,lt=100"`
}

func RegisterUser(c *gin.Context) {
	resp := new(Resp)
	req := new(RegisterUserRequest)
	defer func() {
		JSONL(c, 200, req, resp)
	}()

	if errResp := ParseJSON(c, req); errResp != nil {
		resp.Error = errResp
		return
	}

	// validate
	err := validate.Struct(req)
	if err != nil {
		flog.Log.Errorf("RegisterUser err: %s", err.Error())
		resp.Error = Error(ParasError, err.Error())
		return
	}

	u := new(model.User)

	// name check
	u.Name = req.Name
	repeat, err := u.IsNameRepeat()
	if err != nil {
		flog.Log.Errorf("RegisterUser err: %s", err.Error())
		resp.Error = Error(DBError, err.Error())
		return
	}
	if repeat {
		flog.Log.Errorf("RegisterUser err: %s", "name already use by other")
		resp.Error = Error(ParasError, "name already use by other")
		return
	}

	// email check
	u.Email = req.Email
	repeat, err = u.IsEmailRepeat()
	if err != nil {
		flog.Log.Errorf("RegisterUser err: %s", err.Error())
		resp.Error = Error(DBError, err.Error())
		return
	}
	if repeat {
		flog.Log.Errorf("RegisterUser err: %s", "email already use by other")
		resp.Error = Error(ParasError, "email already use by other")
		return
	}

	// if image not empty
	if req.ImagePath != "" {
		p := new(model.Picture)
		p.Url = req.ImagePath
		ok, err := p.Exist()
		if err != nil {
			// db err
			flog.Log.Errorf("RegisterUser err:%s", err.Error())
			resp.Error = Error(DBError, err.Error())
			return
		}

		if !ok {
			// not found
			flog.Log.Errorf("RegisterUser err: image not exist")
			resp.Error = Error(ParasError, "image url not exist")
			return
		}

		u.HeadPhoto = req.ImagePath
	}

	u.ActivateMd5 = util.Md5(u.Email)
	u.CreateTime = time.Now().Unix()
	u.Describe = req.Describe
	u.ActivateExpired = time.Now().Add(48 * time.Hour).Unix()
	u.NickName = req.NickName
	u.Password = req.Password
	u.Gender = req.Gender
	u.WeChat = req.WeChat
	u.QQ = req.QQ
	u.Github = req.Github
	u.WeiBo = req.WeiBo

	// send email
	mm := new(mail.Message)
	mm.Sender = config.FafaConfig.MailConfig
	mm.To = u.Email
	mm.ToName = u.NickName
	mm.Body = fmt.Sprintf(mm.Body, config.FafaConfig.Domain+"/verify?code="+u.ActivateMd5)
	err = mm.Sent()
	if err != nil {
		flog.Log.Errorf("RegisterUser err:%s", err.Error())
		resp.Error = Error(EmailError, err.Error())
		return
	}
	err = u.InsertOne()
	if err != nil {
		// db err
		flog.Log.Errorf("RegisterUser err:%s", err.Error())
		resp.Error = Error(DBError, err.Error())
		return
	}

	resp.Flag = true
	resp.Data = u
}

func ActivateUser(c *gin.Context) {
	resp := new(Resp)
	code := c.Query("code")
	defer func() {
		LogAlone(c, nil, resp)
	}()

	if code == "" {
		flog.Log.Errorf("ActivateUser err:%s", "code empty")
		resp.Error = Error(ParasError, "code empty")
		c.String(200, "code empty")
		return
	}

	u := new(model.User)
	u.ActivateMd5 = code

	exist, err := u.IsActivateCodeExist()
	if err != nil {
		flog.Log.Errorf("ActivateUser err:%s", err.Error())
		resp.Error = Error(ParasError, "db err")
		c.String(200, "db err")
		return
	}

	if !exist {
		flog.Log.Errorf("ActivateUser err:%s", "not exist code")
		resp.Error = Error(LazyError, "code not found")
		c.String(200, "code not found")
		return
	}

	if u.Status != 0 {
		c.Redirect(302, config.FafaConfig.Domain)
		return
	}

	if u.ActivateExpired < time.Now().Unix() {
		flog.Log.Errorf("ActivateUser err:%s", "code expired")
		resp.Error = Error(LazyError, "code expired")
		c.String(200, "code expired, resent email:<a href='%s/activate/code?code=%s'>Here</a>", config.FafaConfig.Domain, code)
		return
	} else {
		u.Status = 1
		err = u.UpdateStatus()
		if err != nil {
			flog.Log.Errorf("ActivateUser err:%s", err.Error())
			resp.Error = Error(ParasError, "db err")
			c.String(200, "db err")
			return
		}
		err = SetUserSession(c, u)
		if err != nil {
			flog.Log.Errorf("ActivateUser err:%s", err.Error())
			resp.Error = Error(I500, ErrorMap[I500])
			c.String(200, ErrorMap[I500])
			return
		}
	}

	c.Redirect(302, config.FafaConfig.Domain)

}

func ResendActivateCodeToUser(c *gin.Context) {
	resp := new(Resp)
	code := c.Query("code")
	defer func() {
		LogAlone(c, nil, resp)
	}()

	if code == "" {
		resp.Error = Error(ParasError, "code empty")
		c.String(200, "code empty")
		return
	}

	u := new(model.User)
	u.ActivateMd5 = code

	exist, err := u.IsActivateCodeExist()
	if err != nil {
		flog.Log.Errorf("ResendUser err:%s", err.Error())
		resp.Error = Error(ParasError, "db err")
		c.String(200, "db err")
		return
	}
	if !exist {
		flog.Log.Errorf("ResendUser err:%s", "not exist code")
		resp.Error = Error(LazyError, "code not found")
		c.String(200, "code not found")
		return
	}

	if u.Status != 0 {
	} else if u.ActivateExpired > time.Now().Unix() {
		flog.Log.Errorf("ResendUser err:%s", "code not expired")
		resp.Error = Error(LazyError, "code not expired")
		c.String(200, "code not expired")
		return
	}

	err = u.UpdateActivateCode()
	if err != nil {
		flog.Log.Errorf("ResendUser err:%s", err.Error())
		resp.Error = Error(ParasError, "db err")
		c.String(200, "db err")
		return
	}

	// send email
	mm := new(mail.Message)
	mm.Sender = config.FafaConfig.MailConfig
	mm.To = u.Email
	mm.ToName = u.NickName
	mm.Body = fmt.Sprintf(mm.Body, config.FafaConfig.Domain+"/activate?code="+u.ActivateMd5)
	err = mm.Sent()
	if err != nil {
		flog.Log.Errorf("ResendUser err:%s", err.Error())
		resp.Error = Error(EmailError, err.Error())
		return
	}

	c.String(200, "email code reset")
}

type ForgetPasswordRequest struct {
	Email string `json:"email" validate:"required,email"`
}

func ForgetPassword(c *gin.Context) {
	resp := new(Resp)
	req := new(ForgetPasswordRequest)
	defer func() {
		JSONL(c, 200, req, resp)
	}()

	if errResp := ParseJSON(c, req); errResp != nil {
		resp.Error = errResp
		return
	}

	// validate
	err := validate.Struct(req)
	if err != nil {
		flog.Log.Errorf("RegisterUser err: %s", err.Error())
		resp.Error = Error(ParasError, err.Error())
		return
	}

	u := new(model.User)
	u.Email = req.Email
	ok, err := u.GetUserByEmail()
	if err != nil {
		flog.Log.Errorf("ForgetPassword err:%s", err.Error())
		resp.Error = Error(DBError, ErrorMap[DBError])
		return
	}
	if !ok {
		flog.Log.Errorf("ForgetPassword err:%s", "not found")
		resp.Error = Error(DbNotFound, ErrorMap[DbNotFound])
		return
	}

	if u.CodeExpired < time.Now().Unix() {
		err = u.UpdateCode()
		if err != nil {
			flog.Log.Errorf("ForgetPassword err:%s", err.Error())
			resp.Error = Error(DBError, ErrorMap[DBError])
			return
		}

		// send email
		mm := new(mail.Message)
		mm.Sender = config.FafaConfig.MailConfig
		mm.To = u.Email
		mm.ToName = u.NickName
		mm.Body = "code is: " + u.Code
		err = mm.Sent()
		if err != nil {
			flog.Log.Errorf("ForgetPassword err:%s", err.Error())
			resp.Error = Error(EmailError, err.Error())
			return
		}

	} else {
		flog.Log.Errorf("ForgetPassword err:%s", "time not reach")
		resp.Error = Error(TimeNotReachError, ErrorMap[TimeNotReachError])
		return
	}

	resp.Flag = true
}

type ChangePasswordRequest struct {
	Email    string `json:"email" validate:"required,email"`
	Code     string `json:"code" validate:"required,lt=9,gt=5"`
	Password string `json:"password" validate:"alphanumunicode,gt=5,lt=17"`
}

func ChangePassword(c *gin.Context) {
	resp := new(Resp)
	req := new(ChangePasswordRequest)
	defer func() {
		JSONL(c, 200, req, resp)
	}()

	if errResp := ParseJSON(c, req); errResp != nil {
		resp.Error = errResp
		return
	}

	// validate
	err := validate.Struct(req)
	if err != nil {
		flog.Log.Errorf("ChangePassword err: %s", err.Error())
		resp.Error = Error(ParasError, err.Error())
		return
	}

	u := new(model.User)
	u.Email = req.Email
	ok, err := u.GetUserByEmail()
	if err != nil {
		flog.Log.Errorf("ChangePassword err:%s", err.Error())
		resp.Error = Error(DBError, ErrorMap[DBError])
		return
	}
	if !ok {
		flog.Log.Errorf("ChangePassword err:%s", "not found")
		resp.Error = Error(DbNotFound, "email")
		return
	}

	if u.Code == req.Code {
		u.Password = req.Password
		err = u.UpdatePassword()
		if err != nil {
			flog.Log.Errorf("ChangePassword err:%s", err.Error())
			resp.Error = Error(DBError, ErrorMap[DBError])
			return
		}
	} else {
		flog.Log.Errorf("ChangePassword err:%s", "code wrong")
		resp.Error = Error(CodeWrong, "not valid")
		return
	}

	DeleteUserSession(c)
	c.SetCookie("auth", "", 0, "", "", false, false)
	resp.Flag = true
}

type UpdateUserRequest struct {
	NickName  string `json:"nick_name" validate:"omitempty,gt=1,lt=50"`
	WeChat    string `json:"wechat" validate:"omitempty,alphanumunicode,gt=3,lt=30"`
	WeiBo     string `json:"weibo" validate:"omitempty,url"`
	Github    string `json:"github" validate:"omitempty,url"`
	QQ        string `json:"qq" validate:"omitempty,numeric,gt=6,lt=12"`
	Gender    int    `json:"gender" validate:"oneof=0 1 2"`
	Describe  string `json:"describe" validate:"omitempty,lt=200"`
	ImagePath string `json:"image_path" validate:"omitempty,lt=100"`
}

func UpdateUser(c *gin.Context) {
	resp := new(Resp)
	req := new(UpdateUserRequest)
	defer func() {
		JSONL(c, 200, req, resp)
	}()

	if errResp := ParseJSON(c, req); errResp != nil {
		resp.Error = errResp
		return
	}

	// validate
	err := validate.Struct(req)
	if err != nil {
		flog.Log.Errorf("UpdateUser err: %s", err.Error())
		resp.Error = Error(ParasError, err.Error())
		return
	}

	u := new(model.User)

	// if image not empty
	if req.ImagePath != "" {
		p := new(model.Picture)
		p.Url = req.ImagePath
		ok, err := p.Exist()
		if err != nil {
			// db err
			flog.Log.Errorf("UpdateUser err:%s", err.Error())
			resp.Error = Error(DBError, err.Error())
			return
		}

		if !ok {
			// not found
			flog.Log.Errorf("UpdateUser err: image not exist")
			resp.Error = Error(ParasError, "image url not exist")
			return
		}

		u.HeadPhoto = req.ImagePath
	}

}

func TakeUser(c *gin.Context) {
	resp := new(Resp)
	defer func() {
		JSONL(c, 200, nil, resp)
	}()

	u, err := GetUserSession(c)
	if err != nil {
		flog.Log.Errorf("TakeUser err:%s", "session not found")
		resp.Error = Error(LazyError, "session not found")
		return
	}
	resp.Flag = true
	resp.Data = u
}

func ListUser(c *gin.Context) {
	resp := new(Resp)
	defer func() {
		JSONL(c, 200, nil, resp)
	}()
}
