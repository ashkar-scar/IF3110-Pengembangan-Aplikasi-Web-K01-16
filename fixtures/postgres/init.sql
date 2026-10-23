CREATE ROLE target_user LOGIN PASSWORD 'target_dev' NOSUPERUSER NOCREATEDB NOCREATEROLE;
CREATE DATABASE sales OWNER target_user;
CREATE DATABASE hr OWNER target_user;
CREATE DATABASE platform_test OWNER platform;
CREATE DATABASE target_test OWNER target_user;
REVOKE CONNECT ON DATABASE platform FROM PUBLIC;
REVOKE CONNECT ON DATABASE platform_test FROM PUBLIC;

\connect sales
SET ROLE target_user;
CREATE TABLE customers (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    name TEXT NOT NULL,
    email TEXT UNIQUE,
    active BOOLEAN NOT NULL DEFAULT true
);
CREATE TABLE orders (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    customer_id BIGINT NOT NULL REFERENCES customers(id),
    amount NUMERIC(12,2) NOT NULL CHECK (amount >= 0),
    note TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX orders_customer_idx ON orders(customer_id);
INSERT INTO customers(name, email) VALUES ('Rani', 'rani@example.test'), ('Bayu', 'bayu@example.test');
INSERT INTO orders(customer_id, amount, note)
SELECT 1 + (n % 2), n * 12500, CASE WHEN n % 3 = 0 THEN NULL ELSE 'Pesanan ' || n END
FROM generate_series(1,120) n;

\connect hr
SET ROLE target_user;
CREATE TABLE employees (id INTEGER PRIMARY KEY, name TEXT NOT NULL, department TEXT NOT NULL);
INSERT INTO employees VALUES (1, 'Sari', 'Engineering'), (2, 'Dimas', 'Finance');
