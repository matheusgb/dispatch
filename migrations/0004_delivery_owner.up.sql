-- Dono da entrega, para autorização por recurso: um caller só pode
-- consultar o tracking das entregas que ele mesmo criou.
ALTER TABLE deliveries ADD COLUMN created_by_caller text;
