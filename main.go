package main

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"time"

	"github.com/go-chi/chi"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/thedevsaddam/renderer"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

var rnd *renderer.Render
var db *mongo.Database

const (
	// Replace this with your MongoDB Atlas connection string
	uri            string = "mongodb+srv://name:password@gotesting.9nowg.mongodb.net/?retryWrites=true&w=majority&appName=GoTesting"
	dbName         string = "demo_todo"
	collectionName string = "todo"
	port           string = ":7000"
)

type (
	todoModel struct {
		ID        primitive.ObjectID `bson:"_id,omitempty"`
		Title     string             `bson:"title"`
		Completed bool               `bson:"completed"`
		CreatedAt time.Time          `bson:"createAt"`
	}

	todo struct {
		ID        string    `json:"id"`
		Title     string    `json:"title"`
		Completed bool      `json:"completed"`
		CreatedAt time.Time `json:"created_at"`
	}
)

func init() {
	var err error
	clientOptions := options.Client().ApplyURI(uri)
	client, err := mongo.Connect(context.TODO(), clientOptions)
	checkErr(err)

	// Ping the MongoDB server to check if the connection is successful
	err = client.Ping(context.TODO(), nil)
	checkErr(err)

	// Get the desired database
	db = client.Database(dbName)
}

func checkErr(err error) {
	if err != nil {
		log.Fatal(err)
	}
}

// home handler to serve HTML response
func homeHandler(w http.ResponseWriter, r *http.Request) {
	err := rnd.Template(w, http.StatusOK, []string{"static/home.tpl"}, nil)
	checkErr(err)
}

// fetch all the todos from MongoDB
func fetchTodos(w http.ResponseWriter, r *http.Request) {
	todos := []todoModel{}
	collection := db.Collection(collectionName)

	cursor, err := collection.Find(context.TODO(), bson.M{})
	checkErr(err)
	defer cursor.Close(context.TODO())

	for cursor.Next(context.TODO()) {
		var t todoModel
		if err := cursor.Decode(&t); err != nil {
			log.Println(err)
			continue
		}
		todos = append(todos, t)
	}

	todoList := []todo{}
	for _, t := range todos {
		todoList = append(todoList, todo{
			ID:        t.ID.Hex(),
			Title:     t.Title,
			Completed: t.Completed,
			CreatedAt: t.CreatedAt,
		})
	}

	rnd.JSON(w, http.StatusOK, renderer.M{
		"data": todoList,
	})
}

func createTodo(w http.ResponseWriter, r *http.Request) {
	var t todo
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		rnd.JSON(w, http.StatusProcessing, err)
		return
	}

	if t.Title == "" {
		rnd.JSON(w, http.StatusBadRequest, renderer.M{
			"message": "The title is required",
		})
	}

	// Converting the JSON received from frontend to bson todoModel struct to store it in MongoDB
	tm := todoModel{
		ID:        primitive.NewObjectID(),
		Title:     t.Title,
		Completed: false,
		CreatedAt: time.Now(),
	}

	// Send model to DB
	collection := db.Collection(collectionName)
	_, err := collection.InsertOne(context.TODO(), tm)
	if err != nil {
		rnd.JSON(w, http.StatusProcessing, renderer.M{
			"message": "Failed to save todo",
			"error":   err,
		})
		return
	}

	// Send response to frontend
	rnd.JSON(w, http.StatusCreated, renderer.M{
		"message": "Todo was created successfully",
		"todo_id": tm.ID.Hex(),
	})
}

func deleteTodo(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))

	// Validate ObjectId format
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		rnd.JSON(w, http.StatusBadRequest, renderer.M{
			"message": "The id is invalid",
		})
		return
	}

	collection := db.Collection(collectionName)
	_, err = collection.DeleteOne(context.TODO(), bson.M{"_id": objectID})
	if err != nil {
		rnd.JSON(w, http.StatusProcessing, renderer.M{
			"message": "Failed to delete todo",
		})
		return
	}

	rnd.JSON(w, http.StatusOK, renderer.M{
		"message": "Todo deleted successfully",
	})
}

func updateTodo(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))

	// Validate ObjectId format
	objectID, err := primitive.ObjectIDFromHex(id)
	if err != nil {
		rnd.JSON(w, http.StatusBadRequest, renderer.M{
			"message": "The id is invalid",
		})
		return
	}

	var t todo
	if err := json.NewDecoder(r.Body).Decode(&t); err != nil {
		rnd.JSON(w, http.StatusProcessing, err)
		return
	}

	if t.Title == "" {
		rnd.JSON(w, http.StatusBadRequest, renderer.M{
			"message": "The title field is required",
		})
		return
	}

	collection := db.Collection(collectionName)
	_, err = collection.UpdateOne(
		context.TODO(),
		bson.M{"_id": objectID},
		bson.M{"$set": bson.M{"title": t.Title, "completed": t.Completed}},
	)
	if err != nil {
		rnd.JSON(w, http.StatusProcessing, renderer.M{
			"message": "Failed to update the todo",
			"error":   err,
		})
		return
	}

	rnd.JSON(w, http.StatusOK, renderer.M{
		"message": "Todo updated successfully",
	})
}

func main() {
	stopChan := make(chan os.Signal)
	signal.Notify(stopChan, os.Interrupt)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Get("/", homeHandler)
	r.Mount("/todo", todoHandlers())

	srv := &http.Server{
		Addr:         port,
		Handler:      r,
		ReadTimeout:  60 * time.Second,
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		log.Println("Listening on port", port)
		if err := srv.ListenAndServe(); err != nil {
			log.Printf("Listen:%s\n", err)
		}
	}()

	<-stopChan
	log.Println("Shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	srv.Shutdown(ctx)
	defer cancel()
	log.Println("Server stopped gracefully")
}

func todoHandlers() http.Handler {
	rg := chi.NewRouter()
	rg.Group(func(r chi.Router) {
		r.Get("/", fetchTodos)
		r.Post("/", createTodo)
		r.Put("/{id}", updateTodo)
		r.Delete("/{id}", deleteTodo)
	})
	return rg
}
