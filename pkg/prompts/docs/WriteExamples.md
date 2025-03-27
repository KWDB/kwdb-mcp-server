# KWDB Write Query Examples

## Basic Write Operations

```sql
-- Insert a single row
INSERT INTO table_name (column1, column2) VALUES ('value1', 'value2');

-- Insert multiple rows
INSERT INTO table_name (column1, column2) 
VALUES ('value1', 'value2'), ('value3', 'value4');

-- Update rows
UPDATE table_name SET column1 = 'new_value' WHERE condition;

-- Delete rows
DELETE FROM table_name WHERE condition;
```

## Relational Database Operations

```sql
-- Create a relational table with primary key
CREATE TABLE customers (
    id SERIAL PRIMARY KEY,
    name VARCHAR(100) NOT NULL,
    email VARCHAR(100) UNIQUE,
    created_at TIMESTAMPTZ DEFAULT now()
);

-- Add a column
ALTER TABLE customers ADD COLUMN phone VARCHAR(20);

-- Modify a column
ALTER TABLE customers ALTER COLUMN phone SET NOT NULL;

-- Create an index
CREATE INDEX idx_customers_email ON customers (email);

-- Create a unique index
CREATE UNIQUE INDEX idx_customers_phone ON customers (phone);

-- Drop an index
DROP INDEX idx_customers_email;

-- Add a foreign key constraint
ALTER TABLE orders ADD CONSTRAINT fk_customer
    FOREIGN KEY (customer_id) REFERENCES customers (id);

-- Create a view
CREATE VIEW active_customers AS
    SELECT * FROM customers WHERE status = 'active';
```

## Time-Series Database Operations

```sql
-- Create a time-series table with TAGS
CREATE TABLE sensor_data (
    ts TIMESTAMP NOT NULL,
    temperature FLOAT8,
    humidity FLOAT4,
    pressure FLOAT4
) TAGS (
    device_id INT NOT NULL,
    location VARCHAR,
    sensor_type VARCHAR
) PRIMARY TAGS(device_id);

-- Insert data into time-series table (including TAG values)
INSERT INTO sensor_data VALUES (
    '2023-01-01 12:00:00', 22.5, 45.2, 1013.2, 
    1001, 'Building A', 'Temperature'
);

-- Insert multiple rows with the same TAG values
INSERT INTO sensor_data (ts, temperature, humidity, pressure, device_id) 
VALUES 
    ('2023-01-01 12:05:00', 22.7, 45.5, 1013.1, 1001),
    ('2023-01-01 12:10:00', 22.9, 45.8, 1013.0, 1001);

-- Update time-series data (note: updates in time-series tables may be less efficient)
UPDATE sensor_data 
SET temperature = 23.0 
WHERE ts = '2023-01-01 12:00:00' AND device_id = 1001;

-- Delete time-series data
DELETE FROM sensor_data 
WHERE ts BETWEEN '2023-01-01' AND '2023-01-02' AND device_id = 1001;
```

## Schema Modification Operations

```sql
-- Add a column to a table
ALTER TABLE table_name ADD COLUMN new_column TEXT;

-- Drop a column
ALTER TABLE table_name DROP COLUMN column_name;

-- Rename a table
ALTER TABLE table_name RENAME TO new_table_name;

-- Rename a column
ALTER TABLE table_name RENAME COLUMN old_name TO new_name;

-- Change column data type
ALTER TABLE table_name ALTER COLUMN column_name TYPE new_data_type;

-- Add a default value to a column
ALTER TABLE table_name ALTER COLUMN column_name SET DEFAULT default_value;

-- Remove a default value
ALTER TABLE table_name ALTER COLUMN column_name DROP DEFAULT;

-- Add a NOT NULL constraint
ALTER TABLE table_name ALTER COLUMN column_name SET NOT NULL;

-- Remove a NOT NULL constraint
ALTER TABLE table_name ALTER COLUMN column_name DROP NOT NULL;
```

## Transaction Operations

```sql
-- Begin a transaction
BEGIN;

-- Multiple operations within a transaction
INSERT INTO accounts (id, balance) VALUES (1, 1000);
UPDATE accounts SET balance = balance - 100 WHERE id = 1;
INSERT INTO transactions (account_id, amount) VALUES (1, -100);

-- Commit a transaction
COMMIT;

-- Rollback a transaction
ROLLBACK;

-- Create a savepoint
SAVEPOINT my_savepoint;

-- Rollback to a savepoint
ROLLBACK TO SAVEPOINT my_savepoint;
```

## Batch Operations

```sql
-- Bulk insert from a SELECT query
INSERT INTO table_copy (column1, column2)
SELECT column1, column2 FROM source_table WHERE condition;

-- Bulk update
UPDATE products 
SET price = price * 1.1 
WHERE category = 'Electronics';

-- Upsert (insert or update)
INSERT INTO inventory (product_id, quantity) 
VALUES (101, 50)
ON CONFLICT (product_id) 
DO UPDATE SET quantity = inventory.quantity + EXCLUDED.quantity;
``` 