package handler

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"uuid"

	"github.com/jackc/pgx/v5"
	"main.go/internal/models"
	"main.go/internal/repository"
)

type WalletRepository interface {
	GetByID(id string) (*models.Wallet, error)
	CommitTransaction(models.Operation)
}

type WalletHandler struct {
	Repo *repository.PostgresWalletRepository
}

func (h *WalletHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(r.PathValue("id"))
	if err != nil {
		http.Error(w, "Invalid ID format", http.StatusBadRequest)
		return
	}

	if id == uuid.Nil() {
		http.Error(w, "Empty ID", http.StatusBadRequest)
		return
	}

	wallet, err := h.Repo.GetByID(r.Context(), id)
	if err != nil {
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			http.Error(w, models.ErrWalletNotFound.Error(), http.StatusNotFound)
		default:
			fmt.Printf("query wallet error: %v\n", err)
			http.Error(w, "internal error", http.StatusInternalServerError)
		}
		return
	}

	if wallet == nil {
		http.Error(w, models.ErrWalletNotFound.Error(), http.StatusNotFound)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)

	json.NewEncoder(w).Encode(wallet)
}

func (h *WalletHandler) WalletOperation(w http.ResponseWriter, r *http.Request) {
	var operation models.Operation

	err := json.NewDecoder(r.Body).Decode(&operation)
	if err != nil {
		http.Error(w, "invalid request body format", http.StatusBadRequest)
		return
	}

	err = h.Repo.ChangeBalance(r.Context(), operation)
	if err != nil {
		switch {
		case errors.Is(err, models.ErrWalletNotFound):
			http.Error(w, err.Error(), http.StatusBadRequest)
		case errors.Is(err, models.ErrInsufficientFunds):
			http.Error(w, err.Error(), http.StatusConflict)
		case errors.Is(err, models.ErrInvalidOperation):
			http.Error(w, err.Error(), http.StatusBadRequest)
		default:
			fmt.Printf("change balance error: %v", err)
			http.Error(w, "unknown error", http.StatusInternalServerError)
		}
		return
	}
	w.WriteHeader(http.StatusOK)
}
