UPDATE products SET price = price * 100; -- dollars to cents
ALTER TABLE products ALTER COLUMN price TYPE INT;
ALTER TABLE products ALTER COLUMN price SET NOT NULL;
