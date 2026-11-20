CREATE TABLE wallets (
    walletid UUID PRIMARY KEY,
    balance INT NOT NULL CHECK (balance >= 0)
);