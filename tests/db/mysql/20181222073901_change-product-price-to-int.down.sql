ALTER TABLE products MODIFY price DECIMAL(10,2);
UPDATE products SET price = price / 100; -- cents to dollars
