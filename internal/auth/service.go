package auth

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"

	"reni-social/internal/domain"
	"reni-social/internal/utils"
	"reni-social/pkg/config"
	"reni-social/pkg/db"
)

const sessionTTL = 24 * time.Hour

// ──────────────────────────────────────────────────────────────────────────────
// Signup
// ──────────────────────────────────────────────────────────────────────────────

type SignupRequest struct {
	Email     string `json:"email"`
	Password  string `json:"password"`
	FirstName string `json:"first_name"`
	LastName  string `json:"last_name"`
}

type AuthResponse struct {
	Token string       `json:"token"`
	User  *domain.User `json:"user"`
}

// Signup godoc
// @Summary Signup
// @Description Auto-generated thorough documentation for Signup
// @Tags auth
// @Produce json
// @Param req body string true "req"
// @Success 200 {object} AuthResponse "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func Signup(ctx context.Context, req SignupRequest) (*AuthResponse, error) {
	if req.Email == "" || req.Password == "" {
		return nil, fmt.Errorf("email and password are required")
	}
	if len(req.Password) < 8 {
		return nil, fmt.Errorf("password must be at least 8 characters")
	}

	// Check email uniqueness
	var exists bool
	err := db.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE email=$1)`, req.Email).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}
	if exists {
		return nil, fmt.Errorf("email already registered")
	}

	hash, err := bcrypt.GenerateFromPassword([]byte(req.Password), bcrypt.DefaultCost)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	userID := uuid.New()
	now := time.Now()

	_, err = db.DB.ExecContext(ctx, `
		INSERT INTO users (id, email, password_hash, first_name, last_name, created_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`, userID, req.Email, string(hash), req.FirstName, req.LastName, now)
	if err != nil {
		return nil, fmt.Errorf("create user: %w", err)
	}

	token, err := issueSession(userID)
	if err != nil {
		return nil, err
	}

	return &AuthResponse{
		Token: token,
		User: &domain.User{
			ID:        userID,
			Email:     req.Email,
			FirstName: req.FirstName,
			LastName:  req.LastName,
			CreatedAt: now,
		},
	}, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Signin
// ──────────────────────────────────────────────────────────────────────────────

type SigninRequest struct {
	Mail  string `json:"mail"`
	Pword string `json:"pword"`
}

type SigninResponse struct {
	Status     bool   `json:"status"`
	Message    string `json:"message"`
	Data       any    `json:"data"`
	HttpStatus int    `json:"http_status"`
}

type SigninUserData struct {
	ID               int    `json:"id"`
	Token            string `json:"token"`
	Userkey          string `json:"userkey"`
	Accno            string `json:"accno"`
	Mail             string `json:"mail"`
	Username         string `json:"username"`
	UsernameLock     string `json:"username_lock"`
	Fullname         string `json:"fullname"`
	Fname            string `json:"fname"`
	Lname            string `json:"lname"`
	Phone            string `json:"phone"`
	PhoneCountryCode string `json:"phoneCountryCode"`
	Country          any    `json:"country"`
	Gender           string `json:"gender"`
	Bvn              any    `json:"bvn"`
	BvnData          any    `json:"bvn_data"`
	Nin              any    `json:"nin"`
	Pword            any    `json:"pword"`
	Bankname         any    `json:"bankname"`
	Banknumber       any    `json:"banknumber"`
	Activated        string `json:"activated"`
	Timestamp        string `json:"timestamp"`
	Picture          string `json:"picture"`
	PictureName      string `json:"picture_name"`
	Photo            any    `json:"photo"`
	Ref              string `json:"ref"`
	Upline           string `json:"upline"`
	CreditScore      string `json:"credit_score"`
	Rep              int    `json:"rep"`
	IP               string `json:"ip"`
	IPCurrent        string `json:"ip_current"`
	Address          any    `json:"address"`
	AddressPhoto     any    `json:"addressPhoto"`
	IsBankData       int    `json:"isBankData"`
	IsBankStatus     int    `json:"isBank_status"`
	RemindBvn        int    `json:"remind_bvn"`
	Verified         int    `json:"verified"`
	LoginErr         int    `json:"loginErr"`
	Blocked          int    `json:"blocked"`
	Pin              any    `json:"pin"`
	IDType           string `json:"IDType"`
	IDNumber         string `json:"IDNumber"`
	IDPhoto          any    `json:"IDPhoto"`
	IDData           any    `json:"IDData"`
}

// Signin godoc
// @Summary Signin
// @Description Auto-generated thorough documentation for Signin
// @Tags auth
// @Produce json
// @Param req body string true "req"
// @Success 200 {object} SigninResponse "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func Signin(ctx context.Context, req SigninRequest) (*SigninResponse, error) {
	if req.Mail == "" || req.Pword == "" {
		return nil, fmt.Errorf("mail and password are required")
	}

	var res SigninResponse
	err := utils.DoRequest(utils.RequestOptions{
		Method: "POST",
		URL:    config.C.ReniCentralService + "/auth/login",
		Body:   req,
		Headers: map[string]string{
			"Authorization": "Bearer " + config.C.ReniCentralKey,
		},
		Timeout: 30 * time.Second,
	}, &res)
	if err != nil {
		return nil, fmt.Errorf("login call failed: %w", err)
	}

	if !res.Status {
		return &res, nil
	}

	dataBytes, err := json.Marshal(res.Data)
	if err != nil {
		return nil, err
	}
	var data SigninUserData
	if err := json.Unmarshal(dataBytes, &data); err != nil {
		return nil, fmt.Errorf("failed to decode login data: %w", err)
	}

	centralToken := data.Token
	var localUserID uuid.UUID

	// Check if user exists locally in the database by email
	err = db.DB.QueryRowContext(ctx, `
		SELECT id FROM users WHERE email = $1
	`, data.Mail).Scan(&localUserID)

	if err == sql.ErrNoRows {
		// User does not exist locally, so we insert them
		localUserID = uuid.New()
		now := time.Now()

		_, err = db.DB.ExecContext(ctx, `
			INSERT INTO users (
				id, email, phone_number, is_email_verified, is_phone_verified,
				first_name, last_name, fullname, username, gender, created_at, token, avatar_url
			) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		`, localUserID, data.Mail, data.Phone, true, true,
			data.Fname, data.Lname, data.Fullname, data.Username, data.Gender, now, centralToken, data.Picture)
		if err != nil {
			return nil, fmt.Errorf("failed to insert new user locally: %w", err)
		}
	} else if err != nil {
		return nil, fmt.Errorf("database query error: %w", err)
	} else {
		// User exists locally, we update their details with the latest from Reni Central
		_, err = db.DB.ExecContext(ctx, `
			UPDATE users SET
				phone_number = $1,
				first_name = $2,
				last_name = $3,
				fullname = $4,
				username = $5,
				gender = $6,
				token = $7,
				avatar_url = $8
			WHERE id = $9
		`, data.Phone, data.Fname, data.Lname, data.Fullname, data.Username, data.Gender, centralToken, data.Picture, localUserID)
		if err != nil {
			return nil, fmt.Errorf("failed to update user locally: %w", err)
		}
	}

	// Generate local session token
	sessionToken, err := issueSession(localUserID)
	if err != nil {
		return nil, fmt.Errorf("failed to issue session: %w", err)
	}

	// Overwrite token field in response with the local session ID
	data.Token = sessionToken
	res.Data = data

	return &res, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Signout
// ──────────────────────────────────────────────────────────────────────────────

// Signout godoc
// @Summary Signout
// @Description Auto-generated thorough documentation for Signout
// @Tags auth
// @Produce json
// @Param token body string true "token"
// @Success 200 {object} map[string]any "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func Signout(token string) error {
	return utils.DeleteSession(token)
}

// ──────────────────────────────────────────────────────────────────────────────
// Me
// ──────────────────────────────────────────────────────────────────────────────

// GetMe godoc
// @Summary GetMe
// @Description Auto-generated thorough documentation for GetMe
// @Tags auth
// @Produce json
// @Param userID body string true "userID"
// @Success 200 {object} domain.User "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func GetMe(ctx context.Context, userID string) (*domain.User, error) {
	var user domain.User
	err := db.DB.QueryRowContext(ctx, `
		SELECT id, email, first_name, last_name, avatar_url, created_at
		FROM users WHERE id = $1
	`, userID).Scan(
		&user.ID, &user.Email,
		&user.FirstName, &user.LastName, &user.AvatarURL, &user.CreatedAt,
	)
	if err == sql.ErrNoRows {
		return nil, fmt.Errorf("user not found")
	}
	if err != nil {
		return nil, fmt.Errorf("database error: %w", err)
	}
	return &user, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// Session helpers
// ──────────────────────────────────────────────────────────────────────────────

// issueSession generates a cryptographically random token, stores it in Redis,
// and returns the token to be sent to the client.
// issueSession godoc
// @Summary issueSession
// @Description Auto-generated thorough documentation for issueSession
// @Tags auth
// @Produce json
// @Param userID body string true "userID"
// @Success 200 {string} string "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func issueSession(userID uuid.UUID) (string, error) {
	token := utils.RandomAlphaNumeric(64)
	if err := utils.SetSession(token, userID, sessionTTL); err != nil {
		return "", fmt.Errorf("create session: %w", err)
	}
	return token, nil
}

// ──────────────────────────────────────────────────────────────────────────────
// 3-Step Registration
// ──────────────────────────────────────────────────────────────────────────────

type RegisterEmailRequest struct {
	Mail string `json:"mail"`
}

type RegisterEmailResponse struct {
	Status     bool   `json:"status"`
	Message    string `json:"message"`
	Data       any    `json:"data"`
	HttpStatus int    `json:"http_status"`
}

// RegisterEmail godoc
// @Summary RegisterEmail
// @Description Auto-generated thorough documentation for RegisterEmail
// @Tags auth
// @Produce json
// @Param req body string true "req"
// @Success 200 {object} RegisterEmailResponse "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func RegisterEmail(ctx context.Context, req RegisterEmailRequest) (*RegisterEmailResponse, error) {
	var res RegisterEmailResponse
	err := utils.DoRequest(utils.RequestOptions{
		Method: "POST",
		URL:    config.C.ReniCentralService + "/auth/register/email",
		Body:   req,
		Headers: map[string]string{
			"Authorization": "Bearer " + config.C.ReniCentralKey,
		},
		Timeout: 30 * time.Second,
	}, &res)
	if err != nil {
		return nil, fmt.Errorf("register email call failed: %w", err)
	}
	return &res, nil
}

type ActivateEmailRequest struct {
	Mail     string `json:"mail"`
	AuthCode string `json:"authCode"`
}

type ActivateEmailResponse struct {
	Status     bool   `json:"status"`
	Message    string `json:"message"`
	Data       any    `json:"data"`
	HttpStatus int    `json:"http_status"`
}

// ActivateEmail godoc
// @Summary ActivateEmail
// @Description Auto-generated thorough documentation for ActivateEmail
// @Tags auth
// @Produce json
// @Param req body string true "req"
// @Success 200 {object} ActivateEmailResponse "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func ActivateEmail(ctx context.Context, req ActivateEmailRequest) (*ActivateEmailResponse, error) {
	var res ActivateEmailResponse
	err := utils.DoRequest(utils.RequestOptions{
		Method: "POST",
		URL:    config.C.ReniCentralService + "/auth/register/email/activate",
		Body:   req,
		Headers: map[string]string{
			"Authorization": "Bearer " + config.C.ReniCentralKey,
		},
		Timeout: 30 * time.Second,
	}, &res)
	if err != nil {
		fmt.Println("err", err.Error())
		return nil, fmt.Errorf("activate email call failed: %w", err)
	}
	return &res, nil
}

type CompleteRegisterRequest struct {
	Mail     string `json:"mail"`
	Fname    string `json:"fname"`
	Lname    string `json:"lname"`
	Phone    string `json:"phone"`
	Pword    string `json:"pword"`
	Cpword   string `json:"cpword"`
	Username string `json:"username"`
	Gender   string `json:"gender"`
}

type CompleteRegisterResponse struct {
	Status     bool   `json:"status"`
	Message    string `json:"message"`
	Data       any    `json:"data"`
	HttpStatus int    `json:"http_status"`
}

type CompleteRegisterData struct {
	Fname    string `json:"fname"`
	Lname    string `json:"lname"`
	Phone    string `json:"phone"`
	Gender   string `json:"gender"`
	Mail     string `json:"mail"`
	Pword    string `json:"pword"`
	Username string `json:"username"`
	Fullname string `json:"fullname"`
	Token    string `json:"token"`
}

// CompleteRegister godoc
// @Summary CompleteRegister
// @Description Auto-generated thorough documentation for CompleteRegister
// @Tags auth
// @Produce json
// @Param req body string true "req"
// @Success 200 {object} CompleteRegisterResponse "Successful operation"
// @Failure 400 {object} map[string]string "Bad Request"
// @Failure 401 {object} map[string]string "Unauthorized"
// @Failure 403 {object} map[string]string "Forbidden"
// @Failure 404 {object} map[string]string "Not Found"
// @Failure 500 {object} map[string]string "Internal Server Error"
func CompleteRegister(ctx context.Context, req CompleteRegisterRequest) (*CompleteRegisterResponse, error) {
	var res CompleteRegisterResponse
	err := utils.DoRequest(utils.RequestOptions{
		Method: "POST",
		URL:    config.C.ReniCentralService + "/auth/register/complete",
		Body:   req,
		Headers: map[string]string{
			"Authorization": "Bearer " + config.C.ReniCentralKey,
		},
		Timeout: 30 * time.Second,
	}, &res)
	if err != nil {
		return nil, fmt.Errorf("complete registration call failed: %w", err)
	}

	if !res.Status {
		// res.Data.Pword = ""
		return &res, nil
	}

	dataBytes, err := json.Marshal(res.Data)
	if err != nil {
		return nil, err
	}
	var data CompleteRegisterData
	if err := json.Unmarshal(dataBytes, &data); err != nil {
		return nil, fmt.Errorf("failed to decode registration data: %w", err)
	}

	// Store central token before we overwrite it
	centralToken := data.Token

	// 1. Generate local user ID
	userID := uuid.New()
	now := time.Now()

	// 2. Save user information locally in database (excluding the password, storing central token)
	_, err = db.DB.ExecContext(ctx, `
		INSERT INTO users (
			id, email, phone_number, is_email_verified, is_phone_verified,
			first_name, last_name, fullname, username, gender, created_at, token
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`, userID, data.Mail, data.Phone, true, true,
		data.Fname, data.Lname, data.Fullname, data.Username, data.Gender, now, centralToken)
	if err != nil {
		return nil, fmt.Errorf("failed to save user locally: %w", err)
	}

	// 3. Generate local session ID and store in Redis mapped to local user ID
	sessionToken, err := issueSession(userID)
	if err != nil {
		return nil, fmt.Errorf("failed to issue session: %w", err)
	}

	// 4. Overwrite token field in response with the local session ID
	data.Token = sessionToken
	data.Pword = ""
	res.Data = data

	return &res, nil
}
