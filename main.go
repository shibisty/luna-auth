package auth

import (
	"database/sql"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	_ "github.com/jackc/pgx/v5/stdlib"
	"golang.org/x/crypto/bcrypt"

	"auth/src/database/migrations/create_users"
)

var (
	db        *sql.DB
	jwtSecret = []byte(getEnv("SECRET_KEY", "super-secret"))
)

type LoginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type RegisterRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func main() {

	dsn := buildDSN()

	var err error
	db, err = sql.Open("pgx", dsn)
	if err != nil {
		panic(err)
	}

	if err = db.Ping(); err != nil {
		panic(err)
	}

	migrate()

	r := gin.Default()

	api := r.Group("/api/v1")
	{
		api.GET("/ping", ping)
		api.POST("/register", register)
		api.POST("/login", login)
		api.GET("/verify", verify)
	}

	if err := r.Run(":8080"); err != nil {
		panic(err)
	}
}

// -----------------------------------------------------------------------------
// Migrate
// -----------------------------------------------------------------------------

func migrate() {
    prefix := getEnv("DB_PREFIX", "")

    if err := create_users.Up(db, prefix); err != nil {
        panic(err)
    }
}

// -----------------------------------------------------------------------------
// Health
// -----------------------------------------------------------------------------

func ping(c *gin.Context) {

	if err := db.Ping(); err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{
			"status": "database unavailable",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "ok",
	})
}

// -----------------------------------------------------------------------------
// Register
// -----------------------------------------------------------------------------

func register(c *gin.Context) {

	var req RegisterRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid payload",
		})
		return
	}

	var exists bool
	prefix := getEnv("DB_PREFIX", "")

	err := db.QueryRow(
		`SELECT EXISTS(
			SELECT 1
			FROM ` + prefix + `users
			WHERE email=$1
		)`,
		req.Email,
	).Scan(&exists)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "database error",
		})
		return
	}

	if exists {
		c.JSON(http.StatusConflict, gin.H{
			"error": "user already exists",
		})
		return
	}

	hash, err := bcrypt.GenerateFromPassword(
		[]byte(req.Password),
		bcrypt.DefaultCost,
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "password hash failed",
		})
		return
	}

	_, err = db.Exec(
		`INSERT INTO users(email,password_hash)
		 VALUES($1,$2)`,
		req.Email,
		string(hash),
	)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "user creation failed",
		})
		return
	}

	c.JSON(http.StatusCreated, gin.H{
		"message": "user registered successfully",
	})
}

// -----------------------------------------------------------------------------
// Login
// -----------------------------------------------------------------------------

func login(c *gin.Context) {

	var req LoginRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"error": "invalid payload",
		})
		return
	}

	var (
		id           int64
		passwordHash string
	)
	
	prefix := getEnv("DB_PREFIX", "")
	err := db.QueryRow(
		`SELECT id,password_hash
		   FROM ` + prefix + `users
		  WHERE email=$1`,
		req.Email,
	).Scan(&id, &passwordHash)

	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "invalid credentials",
		})
		return
	}

	if bcrypt.CompareHashAndPassword(
		[]byte(passwordHash),
		[]byte(req.Password),
	) != nil {

		c.JSON(http.StatusUnauthorized, gin.H{
			"error": "invalid credentials",
		})
		return
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{
		"sub": id,
		"exp": time.Now().Add(15 * time.Minute).Unix(),
	})

	signed, err := token.SignedString(jwtSecret)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"error": "token generation failed",
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"access_token": signed,
		"token_type":  "bearer",
	})
}

// -----------------------------------------------------------------------------
// Verify
// -----------------------------------------------------------------------------

func verify(c *gin.Context) {

	tokenStr := c.Query("token")

	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		return jwtSecret, nil
	})

	if err != nil || !token.Valid {
		c.JSON(http.StatusUnauthorized, gin.H{
			"valid": false,
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"valid":  true,
		"claims": token.Claims,
	})
}

// -----------------------------------------------------------------------------
// Env
// -----------------------------------------------------------------------------

func buildDSN() string {
	parts := []string{
		fmt.Sprintf("host=%s", getEnv("DB_HOST", "localhost")),
		fmt.Sprintf("port=%s", getEnv("DB_PORT", "5432")),
		fmt.Sprintf("user=%s", getEnv("DB_USER", "postgres")),
		fmt.Sprintf("password=%s", getEnv("DB_PASSWORD", "")),
		fmt.Sprintf("dbname=%s", getEnv("DB_NAME", "auth_db")),
		fmt.Sprintf("sslmode=%s", getEnv("DB_SSLMODE", "disable")),
	}

	if value := getEnv("DB_SSLROOTCERT", ""); value != "" {
		parts = append(parts, fmt.Sprintf("sslrootcert=%s", value))
	}
	if value := getEnv("DB_SSLCERT", ""); value != "" {
		parts = append(parts, fmt.Sprintf("sslcert=%s", value))
	}
	if value := getEnv("DB_SSLKEY", ""); value != "" {
		parts = append(parts, fmt.Sprintf("sslkey=%s", value))
	}
	if value := getEnv("DB_SSLPASSWORD", ""); value != "" {
		parts = append(parts, fmt.Sprintf("sslpassword=%s", value))
	}

	return strings.Join(parts, " ")
}

func getEnv(key, fallback string) string {

	if value := os.Getenv(key); value != "" {
		return value
	}

	return fallback
}
