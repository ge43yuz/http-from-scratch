package main

import (
	"httpServer/internal/http1"
	"httpServer/internal/router"
	"net/http"
	"slices"
	"strconv"
	"strings"
)

const addr = ":8080"

func main() {
	/*
		http.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
			body := "hello"
			w.Header().Set("Content-Length", fmt.Sprintf("%d", len(body)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(body))
		})

		customMux := router.NewCustomMux()
		fmt.Printf("HTTP server listening on %s...\n", addr)
		if err := http1.ListenAndServe(addr, customMux); err != nil {
			log.Fatalf("Server error: %v", err)
		}
	*/
	a := NewUserRepo()
	a.addUser(1, 13, "User1")
	a.addUser(5, 13, "User2")
	a.addUser(9, 13, "User3")
	a.addUser(4, 13, "User4")
	a.addUser(2, 13, "User5")
	a.addUser(6, 13, "User6")
	customMux := router.NewCustomMux()
	customMux.Handle(http.MethodGet, "/users", a)
	customMux.Handle(http.MethodPost, "/users", a)

	err := http1.ListenAndServe(addr, customMux)
	if err != nil {
		return
	}

}

type User struct {
	id   uint
	age  uint
	name string
}

func NewUser(id uint, name string, age uint) *User {
	return &User{id: id, name: name, age: age}
}

type UserRepo struct {
	users []*User
}

func NewUserRepo() *UserRepo {
	return &UserRepo{users: make([]*User, 0)}
}

func (u *UserRepo) addUser(id uint, age uint, name string) {
	userToAdd := NewUser(id, name, age)
	if slices.Contains(u.users, userToAdd) {
		return
	}
	u.users = append(u.users, userToAdd)
}
func (u *UserRepo) getUsers(request *http.Request) string {
	if request.URL.RawQuery != "" {
		for i := range u.users {
			if strconv.Itoa(int(u.users[i].id)) == request.URL.RawQuery {
				var tmp strings.Builder
				tmp.WriteString(u.users[i].name)
				tmp.WriteString(" ")
				return tmp.String()
			}
		}

	}
	var tmp strings.Builder

	for i := range u.users {
		tmp.WriteString(u.users[i].name)
	}
	return tmp.String()
}

func (u UserRepo) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	writer.Header().Set("Content-Type", "text/plain")
	writer.WriteHeader(http.StatusOK)

	switch request.Method {
	case http.MethodGet:
		writer.Write([]byte(u.getUsers(request)))
	case http.MethodPost:
		//bodyBytes, _ := io.ReadAll(request.Body)
		//u.setName(string(bodyBytes))
		//writer.Write([]byte(u.getUser(request)))
	}
}
