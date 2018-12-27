CREATE TABLE products_tmp AS SELECT id, name, price FROM products;
DROP TABLE products;
ALTER TABLE  products_tmp RENAME TO products;
