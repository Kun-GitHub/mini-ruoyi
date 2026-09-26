package handler

import "github.com/gin-gonic/gin"

type Response struct {
	Code int         `json:"code"`
	Msg  string      `json:"msg"`
	Data interface{} `json:"data,omitempty"`
}

func Success(c *gin.Context, data interface{}) {
	c.JSON(200, Response{Code: 0, Msg: "ok", Data: data})
}

func Fail(c *gin.Context, httpCode int, msg string) {
	c.JSON(httpCode, Response{Code: 1, Msg: msg})
}
