package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"github.com/google/uuid"
	_ "github.com/newrelic/go-agent/v3/integrations/nrmysql"
	"github.com/newrelic/go-agent/v3/newrelic"
	"github.com/newrelic/newrelic-opencensus-exporter-go/nrcensus"
	"github.com/soichisumi/go-util/logger"
	"go.opencensus.io/trace"
	"go.uber.org/zap"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"os"
	"regexp"
)

const (
	defaultPort = "8080"
	//defaultDBUser = "root"
	//defaultDBPassword = ""
	//defaultDBHost = ""
	dataSourceName = "root:@tcp(db:3306)/test"
	driverName     = "nrmysql"
)

var (
	db            *sql.DB
	captureUserID *regexp.Regexp
	app           *newrelic.Application

	//stmtCreateUser *sql.Stmt
	//stmtGetUser *sql.Stmt
	//stmtListUser *sql.Stmt
)

func init() {
	// ?: 0 or more. fewer is preferred
	_captureUserID := regexp.MustCompile("^/users/([a-fA-F0-9]{8}-[a-fA-F0-9]{4}-4[a-fA-F0-9]{3}-[8|9|aA|bB][a-fA-F0-9]{3}-[a-fA-F0-9]{12})?.*?")
	captureUserID = _captureUserID
}

type User struct {
	ID    string `json:"id"`
	Email string `json:"email"`
	Name  string `json:"name"`
}

func logInterceptor(f func(w http.ResponseWriter, r *http.Request)) func(w http.ResponseWriter, r *http.Request) {
	return func(w http.ResponseWriter, r *http.Request) {
		//https://stackoverflow.com/questions/31884093/read-multiple-time-a-reader
		b := bytes.NewBuffer(make([]byte, 0))
		tr := io.TeeReader(r.Body, b)

		var body string
		if r.Method == http.MethodPost {
			_body, err := ioutil.ReadAll(tr)
			if err != nil {
				logger.Error("", zap.Error(err))
			} else {
				body = string(_body)
			}
			defer r.Body.Close()
		}

		r.Body = ioutil.NopCloser(b)

		logger.Info(
			"request received",
			zap.String("path", r.URL.Path),
			zap.String("query", r.URL.Query().Encode()),
			zap.Any("headers", r.Header),
			zap.String("body", body))

		f(w, r)
	}
}

func rootHandler(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func createUser(ctx context.Context, u User) error {
	res, err := db.ExecContext(ctx, "INSERT INTO users(id, email, name) VALUES(?, ?, ?)", u.ID, u.Email, u.Name)
	if err != nil {
		return err
	}
	logger.Info("user created.", zap.Any("", res))
	return nil
}

func listUsers(ctx context.Context) ([]User, error) {
	rows, err := db.QueryContext(ctx, "SELECT * FROM users")
	if err != nil {
		return nil, err
	}
	var res []User
	for rows.Next() {
		var (
			id    string
			email string
			name  string
		)
		if err := rows.Scan(&id, &email, &name); err != nil {
			return nil, err
		}
		res = append(res, User{
			ID:    id,
			Email: email,
			Name:  name,
		})
	}
	return res, nil
}

func getUser(ctx context.Context, id string) (User, error) {
	u := User{}
	if err := db.QueryRowContext(ctx, "SELECT * FROM users WHERE id = ?", id).Scan(&u.ID, &u.Email, &u.Name); err != nil {
		return User{}, err
	}
	return u, nil
}

func getUserID(path string) string {
	s := captureUserID.FindStringSubmatch(path)
	if len(s) < 2 { // no match
		return ""
	}
	return s[1]
}

func handleCreateUser(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	logger.Info("create user")

	var u User
	if err := json.NewDecoder(r.Body).Decode(&u); err != nil && err != io.EOF {
		w.WriteHeader(http.StatusBadRequest)
		logger.Error("", zap.Error(err))
		return
	}

	// validate
	if u.Email == "" || u.Name == "" {
		logger.Error("email or name is empty", zap.Any("user", u))
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	u.ID = uuid.New().String()

	if err := createUser(ctx, u); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		logger.Error("", zap.Error(err))
		return
	}
	w.WriteHeader(http.StatusOK)
}

func handleListUsers(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	logger.Info("list users")

	users, err := listUsers(ctx)
	if err != nil {
		logger.Error("", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	res, err := json.Marshal(users)
	if err != nil {
		logger.Error("", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	_, err = w.Write(res)
	if err != nil {
		logger.Error("", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	return
}

func handleGetUser(ctx context.Context, w http.ResponseWriter, r *http.Request) {
	logger.Info("get user")
	user, err := getUser(ctx, getUserID(r.URL.Path))
	if err != nil {
		logger.Error("", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	res, err := json.Marshal(user)
	if err != nil {
		logger.Error("", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	_, err = w.Write(res)
	if err != nil {
		logger.Error("", zap.Error(err))
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	return
}

func usersHandler(w http.ResponseWriter, r *http.Request) {
	logger.Info("user handler")

	ctx := r.Context()

	_db, err := sql.Open(driverName, dataSourceName)
	if err != nil {
		logger.Fatal("", zap.Error(err))
	}
	db = _db

	id := getUserID(r.URL.Path)

	switch r.Method {
	case http.MethodGet:
		if id == "" {
			handleListUsers(ctx, w, r)
		} else {
			handleGetUser(ctx, w, r)
		}
	case http.MethodPost:
		handleCreateUser(ctx, w, r)
	default:
		w.WriteHeader(http.StatusMethodNotAllowed)
	}
	return
}

func main() {
	port := defaultPort
	if os.Getenv("PORT") != "" {
		port = os.Getenv("PORT")
	}

	_db, err := sql.Open(driverName, dataSourceName)
	if err != nil {
		logger.Fatal("", zap.Error(err))
	}
	db = _db

	//newrelic apm
	_app, err := newrelic.NewApplication(
		newrelic.ConfigAppName("newrelic-go-sample"),
		newrelic.ConfigLicense(os.Getenv("NEWRELIC_LICENSE_KEY")),
		newrelic.ConfigDistributedTracerEnabled(true),
		//newrelic.ConfigDebugLogger(os.Stdout),
	)
	if err != nil {
		logger.Fatal("", zap.Error(err))
	}
	app = _app

	exporter, err := nrcensus.NewExporter(
		"NewRelicGoSample",
		os.Getenv("NEWRELIC_API_KEY"),
	)
	if err != nil {
		log.Fatal(err)
	}
	trace.RegisterExporter(exporter)

	http.HandleFunc(newrelic.WrapHandleFunc(app, "/", logInterceptor(rootHandler)))
	http.HandleFunc(newrelic.WrapHandleFunc(app, "/users/", logInterceptor(usersHandler)))

	logger.Info("http-mock-server is listening.", zap.String("port", port))
	if err := http.ListenAndServe(":"+port, nil); err != nil {
		logger.Fatal(err.Error(), zap.Error(err))
	}
}
