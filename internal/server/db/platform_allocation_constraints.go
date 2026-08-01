package db

import (
	"fmt"

	"gorm.io/gorm"
)

type platformAllocationTrigger struct {
	name      string
	table     string
	operation string
	body      string
}

var platformAllocationTriggers = []platformAllocationTrigger{
	{name: "trg_s3_resource_allocation_insert", table: "resource_allocation", operation: "INSERT", body: resourceAllocationInsertTriggerBody},
	{name: "trg_s3_resource_allocation_update", table: "resource_allocation", operation: "UPDATE", body: resourceAllocationUpdateTriggerBody},
	{name: "trg_s3_resource_allocation_delete", table: "resource_allocation", operation: "DELETE", body: `SELECT RAISE(ABORT, 'S3_ALLOCATION_DELETE_FORBIDDEN');`},
	{name: "trg_s3_resource_allocation_item_insert", table: "resource_allocation_item", operation: "INSERT", body: resourceAllocationItemInsertTriggerBody},
	{name: "trg_s3_resource_allocation_item_update", table: "resource_allocation_item", operation: "UPDATE", body: resourceAllocationItemUpdateTriggerBody},
	{name: "trg_s3_resource_allocation_item_delete", table: "resource_allocation_item", operation: "DELETE", body: `SELECT RAISE(ABORT, 'S3_ALLOCATION_ITEM_DELETE_FORBIDDEN');`},
}

const resourceAllocationRelationshipTriggerBody = `
	SELECT CASE WHEN NOT EXISTS (
		SELECT 1 FROM tenant WHERE id = NEW.tenant_id
	) THEN RAISE(ABORT, 'S3_ALLOCATION_TENANT_NOT_FOUND') END;
	SELECT CASE WHEN NOT EXISTS (
		SELECT 1 FROM user WHERE id = NEW.created_by_user_id
	) THEN RAISE(ABORT, 'S3_ALLOCATION_CREATED_BY_NOT_FOUND') END;
	SELECT CASE WHEN NEW.activated_by_user_id IS NOT NULL AND NOT EXISTS (
		SELECT 1 FROM user WHERE id = NEW.activated_by_user_id
	) THEN RAISE(ABORT, 'S3_ALLOCATION_ACTIVATED_BY_NOT_FOUND') END;
	SELECT CASE WHEN NEW.terminated_by_user_id IS NOT NULL AND NOT EXISTS (
		SELECT 1 FROM user WHERE id = NEW.terminated_by_user_id
	) THEN RAISE(ABORT, 'S3_ALLOCATION_TERMINATED_BY_NOT_FOUND') END;
	SELECT CASE WHEN NEW.renewed_from_id IS NOT NULL AND (
		NEW.renewed_from_id = NEW.id OR NOT EXISTS (
			SELECT 1 FROM resource_allocation WHERE id = NEW.renewed_from_id
		)
	) THEN RAISE(ABORT, 'S3_ALLOCATION_RENEWED_FROM_NOT_FOUND') END;`

const resourceAllocationInsertTriggerBody = resourceAllocationRelationshipTriggerBody + `
	SELECT CASE WHEN NEW.state <> 'draft'
		THEN RAISE(ABORT, 'S3_ALLOCATION_INITIAL_STATE_INVALID') END;`

const resourceAllocationUpdateTriggerBody = resourceAllocationRelationshipTriggerBody + `
	SELECT CASE WHEN NEW.row_version <> OLD.row_version + 1
		THEN RAISE(ABORT, 'S3_ALLOCATION_VERSION_INVALID') END;
	SELECT CASE WHEN NEW.created_by_user_id <> OLD.created_by_user_id
		OR NEW.renewed_from_id IS NOT OLD.renewed_from_id
		THEN RAISE(ABORT, 'S3_ALLOCATION_IDENTITY_IMMUTABLE') END;
	SELECT CASE WHEN OLD.state <> 'draft' AND (
		NEW.tenant_id <> OLD.tenant_id OR NEW.mode <> OLD.mode
		OR NEW.valid_from <> OLD.valid_from OR NEW.expires_at IS NOT OLD.expires_at
		OR NEW.contract_ref <> OLD.contract_ref
	) THEN RAISE(ABORT, 'S3_ALLOCATION_IMMUTABLE') END;
	SELECT CASE WHEN NOT (
		(OLD.state = 'draft' AND NEW.state IN ('draft','scheduled','active','revoked'))
		OR (OLD.state = 'scheduled' AND NEW.state IN ('active','expired','revoked'))
		OR (OLD.state = 'active' AND NEW.state IN ('suspended','expired','revoked'))
		OR (OLD.state = 'suspended' AND NEW.state IN ('active','expired','revoked'))
	) THEN RAISE(ABORT, 'S3_ALLOCATION_STATE_TRANSITION_INVALID') END;
	SELECT CASE WHEN NEW.state IN ('scheduled','active','suspended') AND NOT EXISTS (
		SELECT 1 FROM resource_allocation_item WHERE allocation_id = NEW.id
	) THEN RAISE(ABORT, 'S3_ALLOCATION_ITEM_REQUIRED') END;
	SELECT CASE WHEN NEW.state IN ('scheduled','active','suspended') AND EXISTS (
		SELECT 1
		FROM resource_allocation_item current_item
		JOIN resource_scope current_scope ON current_scope.id = current_item.scope_id
		JOIN resource_allocation_item sibling_item
			ON sibling_item.allocation_id = current_item.allocation_id AND sibling_item.id <> current_item.id
		JOIN resource_scope sibling_scope ON sibling_scope.id = sibling_item.scope_id
		WHERE current_item.allocation_id = NEW.id
			AND (current_scope.parent_id = sibling_scope.id OR sibling_scope.parent_id = current_scope.id)
	) THEN RAISE(ABORT, 'S3_ALLOCATION_HIERARCHY_CONFLICT') END;
	SELECT CASE WHEN NEW.state IN ('scheduled','active','suspended') AND EXISTS (
		SELECT 1
		FROM resource_allocation_item current_item
		JOIN resource_allocation_item occupied_item ON occupied_item.scope_id = current_item.scope_id
		JOIN resource_allocation occupied ON occupied.id = occupied_item.allocation_id
		WHERE current_item.allocation_id = NEW.id
			AND occupied.id <> NEW.id
			AND occupied.state IN ('scheduled','active','suspended')
			AND julianday(occupied.valid_from) < COALESCE(julianday(NEW.expires_at), 5373484.499999)
			AND julianday(NEW.valid_from) < COALESCE(julianday(occupied.expires_at), 5373484.499999)
	) THEN RAISE(ABORT, 'S3_ALLOCATION_SCOPE_CONFLICT') END;
	SELECT CASE WHEN NEW.state IN ('scheduled','active','suspended') AND EXISTS (
		SELECT 1
		FROM resource_allocation_item current_item
		JOIN resource_scope current_scope ON current_scope.id = current_item.scope_id
		JOIN resource_allocation_item occupied_item ON occupied_item.scope_id <> current_item.scope_id
		JOIN resource_scope occupied_scope ON occupied_scope.id = occupied_item.scope_id
		JOIN resource_allocation occupied ON occupied.id = occupied_item.allocation_id
		WHERE current_item.allocation_id = NEW.id
			AND occupied.id <> NEW.id
			AND occupied.state IN ('scheduled','active','suspended')
			AND (current_scope.parent_id = occupied_scope.id OR occupied_scope.parent_id = current_scope.id)
			AND julianday(occupied.valid_from) < COALESCE(julianday(NEW.expires_at), 5373484.499999)
			AND julianday(NEW.valid_from) < COALESCE(julianday(occupied.expires_at), 5373484.499999)
	) THEN RAISE(ABORT, 'S3_ALLOCATION_HIERARCHY_CONFLICT') END;`

const resourceAllocationItemRelationshipTriggerBody = `
	SELECT CASE WHEN NOT EXISTS (
		SELECT 1 FROM resource_allocation WHERE id = NEW.allocation_id
	) THEN RAISE(ABORT, 'S3_ALLOCATION_NOT_FOUND') END;
	SELECT CASE WHEN NOT EXISTS (
		SELECT 1 FROM resource_scope
		WHERE id = NEW.scope_id AND row_version = NEW.scope_row_version_snapshot
	) THEN RAISE(ABORT, 'S3_ALLOCATION_SCOPE_NOT_FOUND_OR_STALE') END;
	SELECT CASE WHEN NOT EXISTS (
		SELECT 1 FROM resource_allocation WHERE id = NEW.allocation_id AND state = 'draft'
	) THEN RAISE(ABORT, 'S3_ALLOCATION_ITEM_IMMUTABLE') END;`

const resourceAllocationItemInsertTriggerBody = resourceAllocationItemRelationshipTriggerBody

const resourceAllocationItemUpdateTriggerBody = resourceAllocationItemRelationshipTriggerBody + `
	SELECT CASE WHEN NEW.allocation_id <> OLD.allocation_id
		THEN RAISE(ABORT, 'S3_ALLOCATION_ITEM_IDENTITY_IMMUTABLE') END;`

func ensurePlatformAllocationConstraints(database *gorm.DB) error {
	return database.Transaction(func(tx *gorm.DB) error {
		for _, trigger := range platformAllocationTriggers {
			statement := fmt.Sprintf(
				"CREATE TRIGGER IF NOT EXISTS %s BEFORE %s ON %s FOR EACH ROW BEGIN %s END",
				trigger.name,
				trigger.operation,
				trigger.table,
				trigger.body,
			)
			if err := tx.Exec(statement).Error; err != nil {
				return fmt.Errorf("create Platform allocation constraint trigger %s: %w", trigger.name, err)
			}
		}
		return nil
	})
}
