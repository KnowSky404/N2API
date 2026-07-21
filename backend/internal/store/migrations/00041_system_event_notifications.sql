-- +goose Up

-- +goose StatementBegin
CREATE OR REPLACE FUNCTION n2api_notify_system_event_inserted()
RETURNS TRIGGER
LANGUAGE plpgsql
AS $$
BEGIN
    PERFORM pg_notify('n2api_system_events', NEW.id::text);
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

DROP TRIGGER IF EXISTS system_events_notify_inserted ON system_events;

CREATE TRIGGER system_events_notify_inserted
AFTER INSERT ON system_events
FOR EACH ROW
EXECUTE FUNCTION n2api_notify_system_event_inserted();

-- +goose Down

DROP TRIGGER IF EXISTS system_events_notify_inserted ON system_events;
DROP FUNCTION IF EXISTS n2api_notify_system_event_inserted();
