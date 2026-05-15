package main

import (
	"log"
	"net/http"

	"kimi-ai-proxy/internal/server"
	"kimi-ai-proxy/internal/utils"
)

func main() {
	server.LoadDotEnv(".env")

	port := utils.GetEnv("PORT", "3001")
	mux := http.NewServeMux()
	mux.HandleFunc("/health", server.HandleHealth)
	mux.HandleFunc("/v1/models", server.WithCORS(server.WithAuth(server.HandleModels)))
	mux.HandleFunc("/v1/chat/completions", server.WithCORS(server.WithAuth(server.HandleChatCompletions)))
	mux.HandleFunc("/", server.WithCORS(func(w http.ResponseWriter, r *http.Request) {
		server.WriteJSON(w, http.StatusOK, map[string]string{"name": "kimi-ai-proxy", "status": "ok"})
	}))

	log.Printf("Kimi proxy listening on 0.0.0.0:%s", port)
	log.Fatal(http.ListenAndServe("0.0.0.0:"+port, mux))
}
