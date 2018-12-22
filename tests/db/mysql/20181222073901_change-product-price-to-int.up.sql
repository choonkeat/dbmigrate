UPDATE products SET price = price * 100; -- dollars to cents
ALTER TABLE products MODIFY price INT NOT NULL;
