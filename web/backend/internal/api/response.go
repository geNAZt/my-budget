package api

import (
	"encoding/json"
	"github.com/labstack/echo/v4"
)

func Send(c echo.Context, code int, i interface{}) error {
	c.Response().Header().Set(echo.HeaderContentType, "application/json")
	c.Response().WriteHeader(code)
	return json.NewEncoder(c.Response()).Encode(i)
}

func Bind(c echo.Context, i interface{}) error {
	return json.NewDecoder(c.Request().Body).Decode(i)
}
