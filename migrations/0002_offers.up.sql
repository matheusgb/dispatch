ALTER TABLE deliveries
    ADD COLUMN offered_at       timestamptz,
    ADD COLUMN offer_expires_at timestamptz;
