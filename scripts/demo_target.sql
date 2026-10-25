-- Demo Target Database Setup
DROP DATABASE IF EXISTS demo_target;
CREATE DATABASE demo_target;

\c demo_target;

CREATE TABLE customers (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    email VARCHAR(150) UNIQUE NOT NULL,
    age INT CHECK (age >= 18),
    phone VARCHAR(20)
);

CREATE TABLE orders (
    id SERIAL PRIMARY KEY,
    customer_id INT NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    amount NUMERIC(10, 2) NOT NULL,
    order_date TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_orders_customer_id ON orders(customer_id);

CREATE TABLE "Odd Table With Space" (
    "Item ID" SERIAL PRIMARY KEY,
    "Item Name" VARCHAR(100) NOT NULL
);

-- Masukkan 35 baris customers (> 30 baris), minimal 1 nilai NULL (phone NULL pada customer 1)
DO $$
BEGIN
    FOR i IN 1..35 LOOP
        IF i = 1 THEN
            INSERT INTO customers (name, email, age, phone) VALUES ('Customer ' || i, 'customer' || i || '@example.com', 20 + (i % 30), NULL);
        ELSE
            INSERT INTO customers (name, email, age, phone) VALUES ('Customer ' || i, 'customer' || i || '@example.com', 20 + (i % 30), '0812345678' || LPAD(i::text, 2, '0'));
        END IF;
    END LOOP;
END $$;

-- Masukkan beberapa baris pesanan
INSERT INTO orders (customer_id, amount) VALUES 
(1, 150000.00), 
(1, 200000.50), 
(2, 75000.00), 
(3, 500000.00);

-- Masukkan data ke Odd Table With Space
INSERT INTO "Odd Table With Space" ("Item Name") VALUES 
('Barang Antik A'), 
('Barang Antik B');
