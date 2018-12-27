CREATE TABLE products_dec (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name VARCHAR(255),
    price DECIMAL(10,2)
);
INSERT INTO products_dec (id, name, price) SELECT id, name, price / 100 FROM products;
DROP TABLE products;
ALTER TABLE  products_dec RENAME TO products;
