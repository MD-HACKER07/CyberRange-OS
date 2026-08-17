// Package httpx holds shared HTTP helpers: uniform error envelope,
// pagination parsing, and request-body binding with validation.
package httpx

import (
	"errors"
	"strconv"
	"strings"

	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type ErrorBody struct {
	Error   string `json:"error"`
	Code    string `json:"code,omitempty"`
	Details any    `json:"details,omitempty"`
}

type APIError struct {
	Status  int
	Code    string
	Message string
	Details any
}

func (e *APIError) Error() string { return e.Message }

func NewError(status int, code, msg string) *APIError {
	return &APIError{Status: status, Code: code, Message: msg}
}

func BadRequest(msg string) *APIError     { return NewError(fiber.StatusBadRequest, "bad_request", msg) }
func Unauthorized(msg string) *APIError   { return NewError(fiber.StatusUnauthorized, "unauthorized", msg) }
func Forbidden(msg string) *APIError      { return NewError(fiber.StatusForbidden, "forbidden", msg) }
func NotFound(msg string) *APIError       { return NewError(fiber.StatusNotFound, "not_found", msg) }
func Conflict(msg string) *APIError       { return NewError(fiber.StatusConflict, "conflict", msg) }
func Internal(msg string) *APIError       { return NewError(fiber.StatusInternalServerError, "internal", msg) }
func Unavailable(msg string) *APIError    { return NewError(fiber.StatusServiceUnavailable, "unavailable", msg) }
func TooManyRequests(msg string) *APIError {
	return NewError(fiber.StatusTooManyRequests, "rate_limited", msg)
}

// ErrorHandler is installed as the Fiber global error handler.
func ErrorHandler(c *fiber.Ctx, err error) error {
	var apiErr *APIError
	if errors.As(err, &apiErr) {
		return c.Status(apiErr.Status).JSON(ErrorBody{
			Error:   apiErr.Message,
			Code:    apiErr.Code,
			Details: apiErr.Details,
		})
	}
	var fe *fiber.Error
	if errors.As(err, &fe) {
		return c.Status(fe.Code).JSON(ErrorBody{Error: fe.Message, Code: "http_error"})
	}
	return c.Status(fiber.StatusInternalServerError).JSON(ErrorBody{
		Error: "internal server error",
		Code:  "internal",
	})
}

func Bind[T any](c *fiber.Ctx) (T, error) {
	var body T
	if err := c.BodyParser(&body); err != nil {
		return body, BadRequest("invalid request body: " + err.Error())
	}
	return body, nil
}

func ParamUUID(c *fiber.Ctx, name string) (uuid.UUID, error) {
	raw := c.Params(name)
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, BadRequest("invalid " + name)
	}
	return id, nil
}

func QueryUUID(c *fiber.Ctx, name string) (uuid.UUID, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return uuid.Nil, false
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return uuid.Nil, false
	}
	return id, true
}

type Page struct {
	Limit  int
	Offset int
}

func ParsePage(c *fiber.Ctx, defLimit, maxLimit int) Page {
	limit := defLimit
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 {
			limit = n
		}
	}
	if limit > maxLimit {
		limit = maxLimit
	}
	offset := 0
	if v := c.Query("offset"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n >= 0 {
			offset = n
		}
	}
	return Page{Limit: limit, Offset: offset}
}

type ListResponse[T any] struct {
	Items []T `json:"items"`
	Total int `json:"total"`
}

func OK(c *fiber.Ctx, data any) error { return c.JSON(data) }

func Created(c *fiber.Ctx, data any) error {
	return c.Status(fiber.StatusCreated).JSON(data)
}

func NoContent(c *fiber.Ctx) error { return c.SendStatus(fiber.StatusNoContent) }
