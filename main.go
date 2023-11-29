// Download the helper library from https://www.twilio.com/docs/go/install
package main

import (
	"encoding/json"
	"fmt"
	"html/template"
	"log"
	"math/rand"
	"net/http"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/boltdb/bolt"
	"github.com/twilio/twilio-go"
	api "github.com/twilio/twilio-go/rest/api/v2010"
)

func HelloServer(w http.ResponseWriter, req *http.Request) {
	err := req.ParseForm()
	if err != nil {
		panic(err)
	}
	for k, v := range req.Form {
		fmt.Printf("%s : %s\n", k, v)
	}

	from, found := req.Form["From"]
	if !found {
		log.Println("From field not found, ignoring")
		return
	}
	log.Println("from found as ", from)
	waId := req.Form.Get("WaId")
	if len(waId) == 0 {
		ReturnMessage(from[0], "Sender not found")
		return
	}
	user, err := GetUser(waId)
	if err != nil {
		fmt.Printf("Error getting user %s\n", err)
		return
	}
	if user == nil {
		log.Println("Create user", waId)
		if user, err = CreateUser(waId); err != nil {
			fmt.Printf("Error creating user %s: %s\n", waId, err)
			return
		}
	} else {
		log.Println("Got User", user)
	}
	log.Println("Got user", user)
	if req.Form.Get("Body") == "0" {
		msg := "0 Menu\n1 See lists\n2 Select list [number]\n3 Show current list\n4 Share List\n- [item number] to delete from list\n9 Create list [name]\nAny other message will be added to current list (you can use commas).\n"
		list, _ := GetList(user.CurrentListId)
		if list == nil {
			msg = fmt.Sprintf("%s\nNo current list selected\n", msg)
		} else {
			msg = fmt.Sprintf("%sCurrent List '%s'\n", msg, list.Name)
		}
		ReturnMessage(from[0], msg)
		return
	}
	if req.Form.Get("Body") == "1" {
		lists, err := GetLists(user)
		if err != nil {
			ReturnMessage(from[0], fmt.Sprintf("Error: %s", err))
			return
		}
		if len(lists) == 0 {
			ReturnMessage(from[0], "No lists")
			return
		}
		keys := make([]string, 0, len(lists))
		for k := range lists {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		resp := ""
		for i, k := range keys {
			resp = fmt.Sprintf("%s%d %s\n", resp, i, lists[k].Name)
		}
		ReturnMessage(from[0], resp)
		return
	}
	if strings.HasPrefix(req.Form.Get("Body"), "- ") {
		if len(user.CurrentListId) == 0 {
			ReturnMessage(from[0], "No list selected")
			return
		}
		i, err := strconv.Atoi(req.Form.Get("Body")[2:])
		if err != nil {
			ReturnMessage(from[0], fmt.Sprintf("Unknown number format: %s", err))
			return
		}
		list, err := GetList(user.CurrentListId)
		if err != nil {
			ReturnMessage(from[0], fmt.Sprintf("Can't load list: %s", err))
			return
		}
		if i >= len(list.Items) {
			ReturnMessage(from[0], "Item number not found")
			return
		}
		list.Items = remove(list.Items, i)
		if err = list.Update(); err != nil {
			ReturnMessage(from[0], fmt.Sprintf("Item deletion error: %s", err))
			return
		}
		msg := fmt.Sprintf("'%s'\n", list.Name)
		for i, item := range list.Items {
			msg = fmt.Sprintf("%s%d %s\n", msg, i, item)
		}
		msg = fmt.Sprintf("%s'-' [number] to delete item\n", msg)
		ReturnMessage(from[0], msg)
		return
	}
	if strings.HasPrefix(req.Form.Get("Body"), "2 ") {
		i, err := strconv.Atoi(req.Form.Get("Body")[2:])
		if err != nil {
			ReturnMessage(from[0], fmt.Sprintf("Unknown list format: %s", err))
			return
		}
		lists, err := GetLists(user)
		if err != nil {
			ReturnMessage(from[0], fmt.Sprintf("Error: %s", err))
			return
		}
		if len(lists) <= i {
			ReturnMessage(from[0], "No such list")
			return
		}
		keys := make([]string, 0, len(lists))
		for k := range lists {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		user.CurrentListId = lists[keys[i]].Id
		if err = user.Update(); err != nil {
			ReturnMessage(from[0], fmt.Sprintf("Can't select list: %s", err))
			return
		}
		ReturnMessage(from[0], fmt.Sprintf("List '%s' selected", lists[keys[i]].Name))
		return
	}
	if strings.HasPrefix(req.Form.Get("Body"), "999 ") {
		listId := req.Form.Get("Body")[4:]
		list, err := GetList(listId)
		if err != nil {
			ReturnMessage(from[0], fmt.Sprintf("Error joining list: %s", err))
			return
		}
		for _, id := range user.ListIds {
			if id == listId {
				ReturnMessage(from[0], fmt.Sprintf("You already are on '%s'", list.Name))
				return
			}
		}
		user.AddList(listId)
		if err = user.Update(); err != nil {
			ReturnMessage(from[0], fmt.Sprintf("Error joining list: %s", err))
			return
		}
		list.Users = append(list.Users, user.WaId)
		if err = list.Update(); err != nil {
			ReturnMessage(from[0], fmt.Sprintf("Error joining list: %s", err))
			return
		}
		ReturnMessage(from[0], fmt.Sprintf("Welcome to '%s'", list.Name))
		return
	}
	if strings.HasPrefix(req.Form.Get("Body"), "9 ") {
		// TODO: Check name does not exist
		listName := req.Form.Get("Body")[2:]
		list, err := CreateList(listName, user)
		if err != nil {
			fmt.Printf("Error creating list %s: %s\n", listName, err)
			ReturnMessage(from[0], fmt.Sprintf("Error creating list '%s': %s", listName, err))
			return
		}
		user.CurrentListId = list.Id
		user.Update()
		ReturnMessage(from[0], fmt.Sprintf("List %s Created", list.Id))
		return
	}
	if len(user.ListIds) == 0 || len(user.CurrentListId) == 0 {
		ReturnMessage(from[0], "No lists exists, or no list selected.\nSend 0 to see menu")
		return
	}
	list, err := GetList(user.CurrentListId)
	if err != nil {
		ReturnMessage(from[0], fmt.Sprintf("Error: %s", err))
		return
	}
	if req.Form.Get("Body") == "3" {
		msg := fmt.Sprintf("'%s'\n", list.Name)
		for i, item := range list.Items {
			msg = fmt.Sprintf("%s%d %s\n", msg, i, item)
		}
		msg = fmt.Sprintf("%s'-' [number] to delete item\n", msg)
		ReturnMessage(from[0], msg)
		return
	}
	if req.Form.Get("Body") == "4" {
		msg := fmt.Sprintf("Share the following link with your contact:\nList '%s'\n%s\n", list.Name, getWaLink(list.Id))
		ReturnMessage(from[0], msg)
		return
	}
	// Assume adding to list
	items := strings.Split(req.Form.Get("Body"), ",")
	list.Items = append(list.Items, items...)
	if err := list.Update(); err != nil {
		ReturnMessage(from[0], fmt.Sprintf("Error: %s", err))
		return
	}
	msg := fmt.Sprintf("'%s'\n", list.Name)
	for i, item := range list.Items {
		msg = fmt.Sprintf("%s%d %s\n", msg, i, item)
	}
	msg = fmt.Sprintf("%s'-' [number] to delete item\n", msg)
	ReturnMessage(from[0], msg)
}

var db *bolt.DB
var listeeNumber string

func getWaLink(listId string) string {
	return fmt.Sprintf("https://wa.me/%s?text=999%%20%s", listeeNumber, listId)
}

func main() {
	listeeNumber = os.Getenv("LISTEE_NUMBER")
	if len(listeeNumber) == 0 {
		panic("No LISTEE_NUMBER environment variable")
	}
	var err error
	db, err = bolt.Open("db/my.db", 0600, nil)
	if err != nil {
		panic(err)
	}
	defer db.Close()
	rand.Seed(time.Now().UnixNano())
	for _, v := range []string{"users", "lists", "users2lists"} {
		log.Println("Creating bucket", v)
		if err = db.Update(func(tx *bolt.Tx) error {
			_, err := tx.CreateBucketIfNotExists([]byte(v))
			if err != nil {
				return fmt.Errorf("create bucket: %s", err)
			}
			return nil
		}); err != nil {
			panic(err)
		}
	}

	// original: https://timberwolf-mastiff-9776.twil.io/demo-reply
	http.HandleFunc("/wa", HelloServer)
	http.HandleFunc("/lst", ListServer)

	//err := http.ListenAndServeTLS(":443", "server.crt", "server.key", nil)
	err = http.ListenAndServe(":8888", nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}

func ReturnMessage(to string, msg string) {
	client := twilio.NewRestClient()

	params := &api.CreateMessageParams{}
	params.SetFrom(fmt.Sprintf("whatsapp:+%s", listeeNumber))
	params.SetBody(msg)
	params.SetTo(to)
	resp, err := client.Api.CreateMessage(params)
	if err != nil {
		fmt.Println(err.Error())
	} else {
		if resp.Sid != nil {
			fmt.Println(*resp.Sid)
		} else {
			fmt.Println(resp.Sid)
		}
	}
}

type ListItem struct {
	Text  string
	Media string
}

type List struct {
	Id    string
	Name  string
	Users []string
	Items []string
}

func GetList(id string) (*List, error) {
	fmt.Println("Get ", id)
	var list List
	loaded := false
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("lists"))
		js := b.Get([]byte(id))
		if len(js) == 0 {
			return nil
		}
		var err error
		if err = json.Unmarshal(js, &list); err == nil {
			loaded = true
		}
		return err
	})
	if !loaded {
		return nil, err
	}
	return &list, err
}

func (l *List) Update() error {
	err := db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("lists"))
		js, _ := json.Marshal(*l)
		fmt.Println("Json", string(js))
		err := b.Put([]byte(l.Id), js)
		return err
	})
	return err
}

func GetLists(user *User) (map[string]*List, error) {
	names := make(map[string]*List)
	for _, id := range user.ListIds {
		fmt.Println("id", id)
		db.View(func(tx *bolt.Tx) error {
			list, err := GetList(id)
			if err != nil {
				return err
			}
			names[list.Name] = list
			return nil
		})
	}
	return names, nil
}

func CreateList(name string, user *User) (*List, error) {
	existingNames, _ := GetLists(user)
	fmt.Println("====>", existingNames)
	i, found := existingNames[name]
	if found {
		return i, nil
	}
	list := List{
		Id:    randSeq(22),
		Name:  name,
		Users: []string{user.WaId},
	}

	if err := list.Update(); err == nil {
		user.ListIds = append(user.ListIds, list.Id)
		return &list, user.Update()
	} else {
		return nil, err
	}
}

type User struct {
	WaId          string
	Paying        bool
	CurrentListId string
	Active        bool
	ActiveDate    int64
	ListIds       []string
}

func GetUser(waId string) (*User, error) {
	var user User
	loaded := false
	err := db.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("users"))
		js := b.Get([]byte(waId))
		if len(js) == 0 {
			return nil
		}
		var err error
		if err = json.Unmarshal(js, &user); err == nil {
			loaded = true
		}
		return err
	})
	if !loaded {
		return nil, err
	}
	return &user, err
}

func (u *User) Update() error {
	err := db.Update(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte("users"))
		js, _ := json.Marshal(*u)
		fmt.Println("Json", string(js))
		err := b.Put([]byte(u.WaId), js)
		return err
	})
	return err
}

func (u *User) AddList(listId string) error {
	u.ListIds = append(u.ListIds, listId)
	return u.Update()
}

func CreateUser(waId string) (*User, error) {
	user := User{
		WaId:       waId,
		Paying:     false,
		Active:     true,
		ActiveDate: time.Now().Unix(),
	}
	return &user, user.Update()
}

var letters = []rune("abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ")

func randSeq(n int) string {
	b := make([]rune, n)
	for i := range b {
		b[i] = letters[rand.Intn(len(letters))]
	}
	return string(b)
}

func remove(s []string, index int) []string {
	ret := make([]string, 0)
	ret = append(ret, s[:index]...)
	return append(ret, s[index+1:]...)
}

func ListServer(w http.ResponseWriter, r *http.Request) {
	p := r.URL.Query()
	listId, found := p["lid"]
	if !found {
		http.Error(w, "list id not found", http.StatusNotAcceptable)
		return
	}
	list, err := GetList(listId[0])
	if err != nil {
		http.Error(w, err.Error(), http.StatusUnprocessableEntity)
		return
	}
	if list == nil {
		http.Error(w, "no list", http.StatusUnprocessableEntity)
		return
	}
	deleteItemIndex, found := p["del"]
	if found {
		intVar, err := strconv.Atoi(deleteItemIndex[0])
		if err != nil {
			http.Error(w, err.Error(), http.StatusNotAcceptable)
			return
		}
		if intVar >= len(list.Items) {
			http.Error(w, "no such item", http.StatusNotAcceptable)
			return
		}
		list.Items = remove(list.Items, intVar)
		if err = list.Update(); err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
	}

	w.Header().Set("Content-Type", "text/html")
	tmpl, err := template.New("table").Funcs(template.FuncMap{"even": even}).Parse(`
	<html><head>
		<meta charset="utf-8">
		<style>
		body {
			  font-family: courier, serif;
			  font-size: 16px;
		  }
		</style>
	</head>
	<table>
	<thead>
	{{.Name}}
	<th></th>
	<th></th>
	</thead>
	<tbody>
	{{range .Items}}
	<tr {{if even .Number}}bgcolor="#dddddd"{{else}}bgcolor="#ababab"{{end}}>
	<td>{{.Name}}</td>
	<td><a href="./lst?lid={{.Id}}&del={{.Number}}" style="text-decoration: none">&#9447</a></td>
	</tr>
	{{end}}
	</tbody>
	</table>
	<script>
	function deleteRow(el) {
		el.parentNode.parentNode.remove();
	}
	</script>
	</html>
	`)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	type rowItem struct {
		Number int
		Name   string
		Id     string
	}

	type Page struct {
		Name  string
		Items []rowItem
	}

	pg := Page{Name: list.Name}

	for i, v := range list.Items {
		pg.Items = append(pg.Items, rowItem{Number: i, Name: v, Id: list.Id})
	}

	if err := tmpl.Execute(w, pg); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
}

func even(i int) bool {
	return i%2 == 0
}
