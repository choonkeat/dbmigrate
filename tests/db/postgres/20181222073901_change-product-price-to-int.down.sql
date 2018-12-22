ALTER TABLE products ALTER COLUMN price TYPE DECIMAL(10,2);
UPDATE products SET price = price / 100; -- cents to dollars
