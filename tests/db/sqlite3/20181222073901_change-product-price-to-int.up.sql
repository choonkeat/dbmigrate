CREATE TABLE products_int (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    name VARCHAR(255),
    price INT
);
INSERT INTO products_int (id, name, price) SELECT id, name, price * 100 FROM products;
DROP TABLE products;
ALTER TABLE  products_int RENAME TO products;
