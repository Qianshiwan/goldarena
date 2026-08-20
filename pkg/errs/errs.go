package errs

import "fmt"

type Code int

const (
	OK              Code = 0
	Unknown         Code = 10001
	InvalidParam    Code = 10002
	Unauthorized    Code = 10003
	Forbidden       Code = 10004
	NotFound        Code = 10005
	TooManyRequests Code = 10006
	Internal        Code = 10007

	// User errors
	UserExists         Code = 10101
	UserNotFound       Code = 10102
	PasswordMismatch   Code = 10103
	InvalidToken       Code = 10104
	TokenExpired       Code = 10105
	InsufficientCoins  Code = 10106

	// Trading errors
	MarketClosed       Code = 10201
	InvalidSymbol      Code = 10202
	InvalidVolume      Code = 10203
	InvalidLeverage    Code = 10204
	InsufficientMargin Code = 10205
	StopOutTriggered   Code = 10206
	PositionNotFound   Code = 10207
	OrderNotFound      Code = 10208

	// Contest errors
	ContestNotFound      Code = 10301
	ContestNotStarted    Code = 10302
	ContestEnded         Code = 10303
	AlreadyRegistered    Code = 10304
	RegistrationDeadline Code = 10305
)

var messages = map[Code]string{
	OK:              "success",
	Unknown:         "unknown error",
	InvalidParam:    "invalid parameter",
	Unauthorized:    "unauthorized",
	Forbidden:       "forbidden",
	NotFound:        "not found",
	TooManyRequests: "too many requests",
	Internal:        "internal server error",

	UserExists:        "user already exists",
	UserNotFound:      "user not found",
	PasswordMismatch:  "password mismatch",
	InvalidToken:      "invalid token",
	TokenExpired:      "token expired",
	InsufficientCoins: "insufficient coins",

	MarketClosed:       "market is closed",
	InvalidSymbol:      "invalid symbol",
	InvalidVolume:      "invalid volume",
	InvalidLeverage:    "invalid leverage",
	InsufficientMargin: "insufficient margin",
	StopOutTriggered:   "stop out triggered",
	PositionNotFound:   "position not found",
	OrderNotFound:      "order not found",

	ContestNotFound:      "contest not found",
	ContestNotStarted:    "contest not started yet",
	ContestEnded:         "contest has ended",
	AlreadyRegistered:    "already registered for this contest",
	RegistrationDeadline: "registration deadline passed",
}

type Error struct {
	Code    Code   `json:"code"`
	Message string `json:"message"`
	Detail  string `json:"detail,omitempty"`
}

func (e *Error) Error() string {
	return fmt.Sprintf("[%d] %s", e.Code, e.Message)
}

func New(code Code, detail string) *Error {
	msg, ok := messages[code]
	if !ok {
		msg = "unknown error"
	}
	return &Error{Code: code, Message: msg, Detail: detail}
}

func NewWithMessage(code Code, message string) *Error {
	return &Error{Code: code, Message: message}
}
