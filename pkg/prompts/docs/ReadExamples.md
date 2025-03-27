# KWDB Read Query Examples

## Basic Queries
```sql
-- Retrieve first 10 rows from a table
SELECT * FROM table_name LIMIT 10;

-- Count total rows in a table
SELECT COUNT(*) FROM table_name;

-- Get execution plan for a query
EXPLAIN ANALYZE SELECT * FROM table_name WHERE condition;

-- Show table structure
SHOW COLUMNS FROM table_name;

-- Get column information from information schema
SELECT column_name, data_type FROM information_schema.columns WHERE table_name = 'table_name';

-- List all tables in the current database
SHOW TABLES;

-- Show table creation statement
SHOW CREATE TABLE table_name;
```

## Relational Database Queries
```sql
-- Query with filtering and sorting
SELECT column1, column2 FROM table_name WHERE condition ORDER BY column1 DESC LIMIT 100;

-- Join multiple tables
SELECT a.column1, b.column2 
FROM table_a a 
JOIN table_b b ON a.id = b.a_id
WHERE a.column1 > 100;

-- Aggregation query
SELECT category, COUNT(*), AVG(amount), MAX(amount)
FROM orders
GROUP BY category
HAVING COUNT(*) > 10
ORDER BY COUNT(*) DESC;

-- Query with subquery
SELECT * FROM customers
WHERE id IN (SELECT customer_id FROM orders WHERE amount > 1000);

-- Common Table Expression (CTE)
WITH high_value_orders AS (
    SELECT customer_id, SUM(amount) as total
    FROM orders
    GROUP BY customer_id
    HAVING SUM(amount) > 10000
)
SELECT c.name, hvo.total
FROM customers c
JOIN high_value_orders hvo ON c.id = hvo.customer_id
ORDER BY hvo.total DESC;

-- Check index usage
SELECT * FROM pg_stat_user_indexes WHERE relname = 'table_name';
```

## Time-Series Database Queries
```sql
-- Query time-series data with time range
SELECT ts, value FROM sensor_data 
WHERE ts BETWEEN '2023-01-01' AND '2023-01-31'
AND tag_name = 'tag_value'
ORDER BY ts;

-- Aggregation by time intervals
SELECT 
    time_bucket('1 hour', ts) AS hour,
    AVG(temperature) AS avg_temp,
    MAX(temperature) AS max_temp
FROM sensor_data
WHERE ts >= NOW() - INTERVAL '24 hours'
AND location_id = 123
GROUP BY hour
ORDER BY hour;

-- Filter by TAG columns
SELECT ts, temperature, humidity
FROM weather_data
WHERE location = 'New York'
AND sensor_type = 'outdoor'
AND ts >= NOW() - INTERVAL '7 days';

-- Latest values per device
SELECT DISTINCT ON (device_id) 
    ts, device_id, temperature, humidity
FROM sensor_readings
WHERE ts >= NOW() - INTERVAL '1 hour'
ORDER BY device_id, ts DESC;

-- Check TAG structure
SHOW CREATE TABLE time_series_table;
```

## System Queries
```sql
-- Check database version
SELECT version();

-- List databases
SHOW DATABASES;

-- Show current database
SELECT current_database();

-- Check active connections
SELECT * FROM pg_stat_activity;

-- Check table sizes
SELECT 
    table_name,
    pg_size_pretty(pg_total_relation_size(table_name)) as total_size
FROM information_schema.tables
WHERE table_schema = 'public'
ORDER BY pg_total_relation_size(table_name) DESC;

-- Check query performance statistics
SELECT * FROM crdb_internal.cluster_queries
WHERE start < (now() - INTERVAL '5 minutes')
ORDER BY cpu_time DESC
LIMIT 10;
``` 