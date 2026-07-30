package db

import (
	"fmt"

	"gorm.io/gorm"
)

type tenantResourceTrigger struct {
	name      string
	table     string
	operation string
	body      string
}

var tenantResourceTriggers = []tenantResourceTrigger{
	{name: "trg_s4_workload_inventory_receipt_insert", table: "workload_inventory_receipt", operation: "INSERT", body: workloadInventoryReceiptTriggerBody},
	{name: "trg_s4_workload_inventory_receipt_update", table: "workload_inventory_receipt", operation: "UPDATE", body: workloadInventoryReceiptUpdateTriggerBody},
	{name: "trg_s4_workload_inventory_receipt_delete", table: "workload_inventory_receipt", operation: "DELETE", body: `SELECT RAISE(ABORT, 'S4_WORKLOAD_RECEIPT_DELETE_FORBIDDEN');`},
	{name: "trg_s4_workload_inventory_batch_insert", table: "workload_inventory_batch", operation: "INSERT", body: workloadInventoryBatchTriggerBody},
	{name: "trg_s4_workload_inventory_batch_update", table: "workload_inventory_batch", operation: "UPDATE", body: workloadInventoryBatchTriggerBody},
	{name: "trg_s4_workload_observation_insert", table: "workload_observation", operation: "INSERT", body: workloadObservationTriggerBody},
	{name: "trg_s4_workload_observation_update", table: "workload_observation", operation: "UPDATE", body: workloadObservationUpdateTriggerBody},
	{name: "trg_s4_workload_observation_source_insert", table: "workload_observation_source", operation: "INSERT", body: workloadObservationSourceTriggerBody},
	{name: "trg_s4_workload_observation_source_update", table: "workload_observation_source", operation: "UPDATE", body: workloadObservationSourceUpdateTriggerBody},
	{name: "trg_s4_workload_observation_source_delete", table: "workload_observation_source", operation: "DELETE", body: `SELECT RAISE(ABORT, 'S4_WORKLOAD_SOURCE_DELETE_FORBIDDEN');`},
	{name: "trg_s4_tenant_resource_insert", table: "tenant_resource", operation: "INSERT", body: tenantResourceRelationshipTriggerBody},
	{name: "trg_s4_tenant_resource_update", table: "tenant_resource", operation: "UPDATE", body: tenantResourceUpdateTriggerBody},
	{name: "trg_s4_tenant_resource_delete", table: "tenant_resource", operation: "DELETE", body: `SELECT RAISE(ABORT, 'S4_TENANT_RESOURCE_DELETE_FORBIDDEN');`},
	{name: "trg_s4_tenant_resource_source_insert", table: "tenant_resource_source", operation: "INSERT", body: tenantResourceSourceRelationshipTriggerBody},
	{name: "trg_s4_tenant_resource_source_update", table: "tenant_resource_source", operation: "UPDATE", body: tenantResourceSourceUpdateTriggerBody},
	{name: "trg_s4_tenant_resource_source_delete", table: "tenant_resource_source", operation: "DELETE", body: `SELECT RAISE(ABORT, 'S4_TENANT_RESOURCE_SOURCE_DELETE_FORBIDDEN');`},
	{name: "trg_s4_tenant_resource_review_insert", table: "tenant_resource_review_decision", operation: "INSERT", body: tenantResourceReviewTriggerBody},
	{name: "trg_s4_tenant_resource_review_update", table: "tenant_resource_review_decision", operation: "UPDATE", body: `SELECT RAISE(ABORT, 'S4_TENANT_RESOURCE_REVIEW_IMMUTABLE');`},
	{name: "trg_s4_tenant_resource_review_delete", table: "tenant_resource_review_decision", operation: "DELETE", body: `SELECT RAISE(ABORT, 'S4_TENANT_RESOURCE_REVIEW_DELETE_FORBIDDEN');`},
	{name: "trg_s4_resource_target_revision_insert", table: "resource_target_revision_v2", operation: "INSERT", body: tenantResourceTargetRevisionInsertTriggerBody},
	{name: "trg_s4_resource_target_revision_update", table: "resource_target_revision_v2", operation: "UPDATE", body: tenantResourceTargetRevisionUpdateTriggerBody},
	{name: "trg_s4_resource_target_revision_delete", table: "resource_target_revision_v2", operation: "DELETE", body: `SELECT RAISE(ABORT, 'S4_TARGET_REVISION_DELETE_FORBIDDEN');`},
	{name: "trg_s4_tenant_access_grant_insert", table: "tenant_access_grant", operation: "INSERT", body: tenantAccessGrantInsertTriggerBody},
	{name: "trg_s4_tenant_access_grant_update", table: "tenant_access_grant", operation: "UPDATE", body: tenantAccessGrantUpdateTriggerBody},
	{name: "trg_s4_tenant_access_grant_delete", table: "tenant_access_grant", operation: "DELETE", body: `SELECT RAISE(ABORT, 'S4_GRANT_DELETE_FORBIDDEN');`},
	{name: "trg_s4_tenant_access_grant_event_insert", table: "tenant_access_grant_event", operation: "INSERT", body: tenantAccessGrantEventTriggerBody},
	{name: "trg_s4_tenant_access_grant_event_update", table: "tenant_access_grant_event", operation: "UPDATE", body: `SELECT RAISE(ABORT, 'S4_GRANT_EVENT_IMMUTABLE');`},
	{name: "trg_s4_tenant_access_grant_event_delete", table: "tenant_access_grant_event", operation: "DELETE", body: `SELECT RAISE(ABORT, 'S4_GRANT_EVENT_DELETE_FORBIDDEN');`},
	{name: "trg_s4_resource_session_insert", table: "resource_session", operation: "INSERT", body: resourceSessionInsertTriggerBody},
	{name: "trg_s4_resource_session_update", table: "resource_session", operation: "UPDATE", body: resourceSessionUpdateTriggerBody},
	{name: "trg_s4_resource_session_delete", table: "resource_session", operation: "DELETE", body: `SELECT RAISE(ABORT, 'S4_RESOURCE_SESSION_DELETE_FORBIDDEN');`},
	{name: "trg_s4_resource_session_event_insert", table: "resource_session_event", operation: "INSERT", body: resourceSessionEventTriggerBody},
	{name: "trg_s4_resource_session_event_update", table: "resource_session_event", operation: "UPDATE", body: `SELECT RAISE(ABORT, 'S4_SESSION_EVENT_IMMUTABLE');`},
	{name: "trg_s4_resource_session_event_delete", table: "resource_session_event", operation: "DELETE", body: `SELECT RAISE(ABORT, 'S4_SESSION_EVENT_DELETE_FORBIDDEN');`},
	{name: "trg_s4_resource_session_termination_insert", table: "resource_session_termination", operation: "INSERT", body: resourceSessionTerminationInsertTriggerBody},
	{name: "trg_s4_resource_session_termination_update", table: "resource_session_termination", operation: "UPDATE", body: resourceSessionTerminationUpdateTriggerBody},
	{name: "trg_s4_resource_session_termination_delete", table: "resource_session_termination", operation: "DELETE", body: `SELECT RAISE(ABORT, 'S4_SESSION_TERMINATION_DELETE_FORBIDDEN');`},
}

const workloadInventoryReceiptTriggerBody = `
	SELECT CASE WHEN NOT EXISTS (
		SELECT 1 FROM technical_resource WHERE id = NEW.source_technical_resource_id
	) THEN RAISE(ABORT, 'S4_WORKLOAD_SOURCE_NOT_FOUND') END;`

const workloadInventoryReceiptUpdateTriggerBody = workloadInventoryReceiptTriggerBody + `
	SELECT CASE WHEN NEW.source_technical_resource_id IS NOT OLD.source_technical_resource_id
		OR NEW.source_epoch IS NOT OLD.source_epoch OR NEW.sequence IS NOT OLD.sequence
		OR NEW.snapshot_id IS NOT OLD.snapshot_id OR NEW.batch_index IS NOT OLD.batch_index
		OR NEW.batch_count IS NOT OLD.batch_count OR NEW.schema_version IS NOT OLD.schema_version
		OR NEW.payload_hash IS NOT OLD.payload_hash OR NEW.lease_expires_at IS NOT OLD.lease_expires_at
		THEN RAISE(ABORT, 'S4_WORKLOAD_RECEIPT_IDENTITY_IMMUTABLE') END;`

const workloadInventoryBatchTriggerBody = `
	SELECT CASE WHEN NOT EXISTS (
		SELECT 1 FROM workload_inventory_receipt WHERE id = NEW.receipt_id
	) THEN RAISE(ABORT, 'S4_WORKLOAD_RECEIPT_NOT_FOUND') END;`

const workloadObservationTriggerBody = `
	SELECT CASE WHEN NOT EXISTS (
		SELECT 1 FROM resource_scope
		WHERE id = NEW.namespace_scope_id AND type = 'namespace'
	) THEN RAISE(ABORT, 'S4_NAMESPACE_SCOPE_NOT_FOUND') END;`

const workloadObservationUpdateTriggerBody = workloadObservationTriggerBody + `
	SELECT CASE WHEN NEW.namespace_scope_id IS NOT OLD.namespace_scope_id
		OR NEW.kind IS NOT OLD.kind OR NEW.stable_key IS NOT OLD.stable_key
		THEN RAISE(ABORT, 'S4_WORKLOAD_OBSERVATION_IDENTITY_IMMUTABLE') END;
	SELECT CASE WHEN NEW.observed_revision NOT IN (OLD.observed_revision, OLD.observed_revision + 1)
		OR NEW.row_version <> OLD.row_version + 1
		THEN RAISE(ABORT, 'S4_WORKLOAD_OBSERVATION_VERSION_INVALID') END;`

const workloadObservationSourceTriggerBody = `
	SELECT CASE WHEN NOT EXISTS (
		SELECT 1
		FROM workload_observation observation
		JOIN resource_scope scope ON scope.id = observation.namespace_scope_id
		JOIN technical_resource technical ON technical.id = NEW.source_technical_resource_id
		JOIN platform_resource_source resource_source
			ON resource_source.platform_resource_id = scope.platform_resource_id
		JOIN supply_candidate candidate ON candidate.id = resource_source.supply_candidate_id
		WHERE observation.id = NEW.workload_observation_id
			AND technical.provider_id = scope.provider_id
			AND (candidate.technical_resource_id = technical.id
				OR candidate.technical_resource_id = technical.parent_id
				OR EXISTS (
					SELECT 1 FROM technical_resource candidate_technical
					WHERE candidate_technical.id = candidate.technical_resource_id
						AND candidate_technical.parent_id = technical.id
				))
	) THEN RAISE(ABORT, 'S4_WORKLOAD_SOURCE_PROVIDER_MISMATCH') END;`

const workloadObservationSourceUpdateTriggerBody = workloadObservationSourceTriggerBody + `
	SELECT CASE WHEN NEW.workload_observation_id IS NOT OLD.workload_observation_id
		OR NEW.source_technical_resource_id IS NOT OLD.source_technical_resource_id
		THEN RAISE(ABORT, 'S4_WORKLOAD_SOURCE_IDENTITY_IMMUTABLE') END;
	SELECT CASE WHEN NEW.source_revision NOT IN (OLD.source_revision, OLD.source_revision + 1)
		OR NEW.row_version <> OLD.row_version + 1
		THEN RAISE(ABORT, 'S4_WORKLOAD_SOURCE_VERSION_INVALID') END;`

const tenantResourceRelationshipTriggerBody = `
	SELECT CASE WHEN NOT EXISTS (
		SELECT 1 FROM tenant WHERE id = NEW.tenant_id
	) THEN RAISE(ABORT, 'S4_TENANT_NOT_FOUND') END;
	SELECT CASE WHEN NOT EXISTS (
		SELECT 1 FROM resource_allocation
		WHERE id = NEW.entitlement_lineage_id AND tenant_id = NEW.tenant_id
			AND renewed_from_id IS NULL
	) THEN RAISE(ABORT, 'S4_RESOURCE_LINEAGE_ROOT_MISMATCH') END;`

const tenantResourceUpdateTriggerBody = tenantResourceRelationshipTriggerBody + `
	SELECT CASE WHEN OLD.visibility_state <> 'pending' AND (
		NEW.tenant_id IS NOT OLD.tenant_id OR NEW.type IS NOT OLD.type
		OR NEW.stable_key IS NOT OLD.stable_key
		OR NEW.entitlement_lineage_id IS NOT OLD.entitlement_lineage_id
	) THEN RAISE(ABORT, 'S4_TENANT_RESOURCE_IDENTITY_IMMUTABLE') END;
	SELECT CASE WHEN NEW.revision <> OLD.revision + 1 OR NEW.row_version <> OLD.row_version + 1
		THEN RAISE(ABORT, 'S4_TENANT_RESOURCE_VERSION_INVALID') END;`

const tenantResourceSourceRelationshipTriggerBody = `
	SELECT CASE WHEN NOT EXISTS (
		SELECT 1
		FROM tenant_resource resource
		JOIN resource_allocation_item item ON item.id = NEW.allocation_item_id
		JOIN resource_allocation allocation ON allocation.id = item.allocation_id
		JOIN workload_observation observation ON observation.id = NEW.workload_observation_id
		WHERE resource.id = NEW.tenant_resource_id
			AND allocation.tenant_id = resource.tenant_id
			AND item.scope_id = observation.namespace_scope_id
	) THEN RAISE(ABORT, 'S4_RESOURCE_SOURCE_CHAIN_MISMATCH') END;
	SELECT CASE WHEN NOT EXISTS (
		WITH RECURSIVE allocation_lineage(id, renewed_from_id, tenant_id) AS (
			SELECT allocation.id, allocation.renewed_from_id, allocation.tenant_id
			FROM resource_allocation_item item
			JOIN resource_allocation allocation ON allocation.id = item.allocation_id
			WHERE item.id = NEW.allocation_item_id
			UNION ALL
			SELECT parent.id, parent.renewed_from_id, parent.tenant_id
			FROM resource_allocation parent
			JOIN allocation_lineage child ON child.renewed_from_id = parent.id
		)
		SELECT 1
		FROM tenant_resource resource
		JOIN allocation_lineage lineage
			ON lineage.id = resource.entitlement_lineage_id
			AND lineage.tenant_id = resource.tenant_id
		JOIN resource_allocation_item root_item ON root_item.allocation_id = lineage.id
		JOIN workload_observation observation ON observation.id = NEW.workload_observation_id
		WHERE resource.id = NEW.tenant_resource_id AND lineage.renewed_from_id IS NULL
			AND root_item.scope_id = observation.namespace_scope_id
			AND NOT EXISTS (
				SELECT 1 FROM allocation_lineage member WHERE member.tenant_id <> resource.tenant_id
			)
	) THEN RAISE(ABORT, 'S4_RESOURCE_SOURCE_LINEAGE_MISMATCH') END;`

const tenantResourceSourceUpdateTriggerBody = tenantResourceSourceRelationshipTriggerBody + `
	SELECT CASE WHEN NEW.tenant_resource_id IS NOT OLD.tenant_resource_id
		OR NEW.allocation_item_id IS NOT OLD.allocation_item_id
		OR NEW.workload_observation_id IS NOT OLD.workload_observation_id
		THEN RAISE(ABORT, 'S4_TENANT_RESOURCE_SOURCE_IDENTITY_IMMUTABLE') END;
	SELECT CASE WHEN NEW.source_revision NOT IN (OLD.source_revision, OLD.source_revision + 1)
		OR NEW.row_version <> OLD.row_version + 1
		THEN RAISE(ABORT, 'S4_TENANT_RESOURCE_SOURCE_VERSION_INVALID') END;`

const tenantResourceReviewTriggerBody = `
	SELECT CASE WHEN NOT EXISTS (
		SELECT 1
		FROM tenant_resource resource
		JOIN tenant_resource_source source ON source.tenant_resource_id = resource.id
		JOIN workload_observation observation ON observation.id = source.workload_observation_id
		WHERE resource.id = NEW.tenant_resource_id
			AND observation.observed_revision = NEW.observation_revision
	) THEN RAISE(ABORT, 'S4_REVIEW_OBSERVATION_REVISION_MISMATCH') END;
	SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM user WHERE id = NEW.actor_user_id)
		OR NOT EXISTS (SELECT 1 FROM user WHERE id = NEW.effective_user_id)
		THEN RAISE(ABORT, 'S4_REVIEW_USER_NOT_FOUND') END;
	SELECT CASE WHEN (NEW.simulation_session_id IS NULL AND NEW.actor_user_id <> NEW.effective_user_id)
		OR (NEW.simulation_session_id IS NOT NULL AND NOT EXISTS (
			SELECT 1
			FROM user_simulation_session simulation
			JOIN tenant_resource resource ON resource.id = NEW.tenant_resource_id
			WHERE simulation.id = NEW.simulation_session_id
				AND simulation.actor_user_id = NEW.actor_user_id
				AND simulation.effective_user_id = NEW.effective_user_id
				AND simulation.scope_type = 'tenant' AND simulation.scope_id = resource.tenant_id
		)) THEN RAISE(ABORT, 'S4_REVIEW_SIMULATION_MISMATCH') END;`

const tenantResourceTargetRevisionInsertTriggerBody = `
	SELECT CASE WHEN NOT EXISTS (
		SELECT 1
		FROM tenant_resource_source source
		JOIN tenant_resource resource ON resource.id = source.tenant_resource_id
		JOIN workload_observation observation ON observation.id = source.workload_observation_id
		JOIN workload_observation_source evidence
			ON evidence.workload_observation_id = observation.id
			AND evidence.source_technical_resource_id = NEW.source_technical_resource_id
		JOIN technical_resource source_technical ON source_technical.id = NEW.source_technical_resource_id
		JOIN technical_resource access_technical ON access_technical.id = NEW.access_technical_resource_id
		JOIN resource_allocation_item item ON item.id = source.allocation_item_id
		JOIN resource_scope scope ON scope.id = item.scope_id
		WHERE source.id = NEW.tenant_resource_source_id
			AND source_technical.provider_id = access_technical.provider_id
			AND EXISTS (
				SELECT 1
				FROM platform_resource_source resource_source
				JOIN supply_candidate candidate ON candidate.id = resource_source.supply_candidate_id
				WHERE resource_source.platform_resource_id = scope.platform_resource_id
					AND (candidate.technical_resource_id = access_technical.id
						OR candidate.technical_resource_id = access_technical.parent_id
						OR EXISTS (
							SELECT 1 FROM technical_resource candidate_technical
							WHERE candidate_technical.id = candidate.technical_resource_id
								AND candidate_technical.parent_id = access_technical.id
						))
			)
			AND NEW.target_type = observation.kind
			AND NEW.observation_revision = observation.observed_revision
			AND NEW.source_revision = source.source_revision
			AND ((resource.type = 'container_service' AND NEW.target_type = 'service_port')
				OR (resource.type = 'container_ssh' AND NEW.target_type = 'container'))
	) THEN RAISE(ABORT, 'S4_TARGET_REVISION_CHAIN_MISMATCH') END;
	SELECT CASE WHEN NEW.revision <> COALESCE((
		SELECT MAX(revision) + 1 FROM resource_target_revision_v2
		WHERE tenant_resource_source_id = NEW.tenant_resource_source_id
	), 1) THEN RAISE(ABORT, 'S4_TARGET_REVISION_SEQUENCE_INVALID') END;`

const tenantResourceTargetRevisionUpdateTriggerBody = `
	SELECT CASE WHEN NEW.id IS NOT OLD.id
		OR NEW.tenant_resource_source_id IS NOT OLD.tenant_resource_source_id
		OR NEW.revision IS NOT OLD.revision OR NEW.target_type IS NOT OLD.target_type
		OR NEW.target_snapshot IS NOT OLD.target_snapshot
		OR NEW.source_technical_resource_id IS NOT OLD.source_technical_resource_id
		OR NEW.access_technical_resource_id IS NOT OLD.access_technical_resource_id
		OR NEW.ready IS NOT OLD.ready OR NEW.observed_at IS NOT OLD.observed_at
		OR NEW.observation_revision IS NOT OLD.observation_revision
		OR NEW.source_revision IS NOT OLD.source_revision OR NEW.created_at IS NOT OLD.created_at
		OR (OLD.superseded_at IS NOT NULL AND NEW.superseded_at IS NOT OLD.superseded_at)
		THEN RAISE(ABORT, 'S4_TARGET_REVISION_IMMUTABLE') END;
	SELECT CASE WHEN NEW.superseded_at IS NOT NULL AND NEW.superseded_at < NEW.observed_at
		THEN RAISE(ABORT, 'S4_TARGET_REVISION_SUPERSEDED_AT_INVALID') END;`

const tenantAccessGrantRelationshipTriggerBody = `
	SELECT CASE WHEN NOT EXISTS (
		SELECT 1 FROM tenant_resource
		WHERE id = NEW.tenant_resource_id AND tenant_id = NEW.tenant_id
	) THEN RAISE(ABORT, 'S4_GRANT_RESOURCE_TENANT_MISMATCH') END;
	SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM user WHERE id = NEW.created_by_user_id)
		OR (NEW.revoked_by_user_id IS NOT NULL
			AND NOT EXISTS (SELECT 1 FROM user WHERE id = NEW.revoked_by_user_id))
		THEN RAISE(ABORT, 'S4_GRANT_ACTOR_NOT_FOUND') END;
	SELECT CASE WHEN (NEW.subject_type = 'user' AND NEW.subject_key <> CAST(NEW.subject_user_id AS TEXT))
		OR (NEW.subject_type = 'group' AND NEW.subject_key <> CAST(NEW.subject_group_id AS TEXT))
		THEN RAISE(ABORT, 'S4_GRANT_SUBJECT_KEY_MISMATCH') END;`

const tenantAccessGrantInsertTriggerBody = tenantAccessGrantRelationshipTriggerBody + `
	SELECT CASE WHEN NEW.subject_type = 'user' AND NOT EXISTS (
		SELECT 1 FROM tenant_membership membership
		WHERE membership.tenant_id = NEW.tenant_id
			AND membership.user_id = NEW.subject_user_id AND membership.enabled = 1
			AND (membership.expires_at IS NULL OR membership.expires_at > NEW.valid_from)
	) THEN RAISE(ABORT, 'S4_GRANT_USER_MEMBERSHIP_MISMATCH') END;
	SELECT CASE WHEN NEW.subject_type = 'group' AND NOT EXISTS (
		SELECT 1 FROM "group" WHERE id = NEW.subject_group_id AND tenant_id = NEW.tenant_id
	) THEN RAISE(ABORT, 'S4_GRANT_GROUP_TENANT_MISMATCH') END;`

const tenantAccessGrantUpdateTriggerBody = tenantAccessGrantRelationshipTriggerBody + `
	SELECT CASE WHEN NEW.tenant_id IS NOT OLD.tenant_id
		OR NEW.tenant_resource_id IS NOT OLD.tenant_resource_id
		OR NEW.subject_type IS NOT OLD.subject_type OR NEW.subject_key IS NOT OLD.subject_key
		OR NEW.subject_user_id IS NOT OLD.subject_user_id
		OR NEW.subject_group_id IS NOT OLD.subject_group_id
		OR NEW.created_by_user_id IS NOT OLD.created_by_user_id
		THEN RAISE(ABORT, 'S4_GRANT_IDENTITY_IMMUTABLE') END;
	SELECT CASE WHEN NEW.revision <> OLD.revision + 1 OR NEW.row_version <> OLD.row_version + 1
		THEN RAISE(ABORT, 'S4_GRANT_VERSION_INVALID') END;
	SELECT CASE WHEN NOT (
		(OLD.status = 'enabled' AND NEW.status IN ('enabled','suspended','revoked','expired'))
		OR (OLD.status = 'suspended' AND NEW.status IN ('enabled','suspended','revoked','expired'))
	) THEN RAISE(ABORT, 'S4_GRANT_STATUS_TRANSITION_INVALID') END;`

const tenantAccessGrantEventTriggerBody = `
	SELECT CASE WHEN NOT EXISTS (
		SELECT 1 FROM tenant_access_grant
		WHERE id = NEW.grant_id AND revision = NEW.grant_revision
	) THEN RAISE(ABORT, 'S4_GRANT_EVENT_REVISION_MISMATCH') END;
	SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM user WHERE id = NEW.actor_user_id)
		OR NOT EXISTS (SELECT 1 FROM user WHERE id = NEW.effective_user_id)
		THEN RAISE(ABORT, 'S4_GRANT_EVENT_USER_NOT_FOUND') END;
	SELECT CASE WHEN (NEW.simulation_session_id IS NULL AND NEW.actor_user_id <> NEW.effective_user_id)
		OR (NEW.simulation_session_id IS NOT NULL AND NOT EXISTS (
			SELECT 1
			FROM user_simulation_session simulation
			JOIN tenant_access_grant grant ON grant.id = NEW.grant_id
			WHERE simulation.id = NEW.simulation_session_id
				AND simulation.actor_user_id = NEW.actor_user_id
				AND simulation.effective_user_id = NEW.effective_user_id
				AND simulation.scope_type = 'tenant' AND simulation.scope_id = grant.tenant_id
		)) THEN RAISE(ABORT, 'S4_GRANT_EVENT_SIMULATION_MISMATCH') END;`

const resourceSessionInsertTriggerBody = `
	SELECT CASE WHEN NOT EXISTS (
		SELECT 1
		FROM tenant_resource resource
		JOIN tenant_resource_source source
			ON source.id = NEW.tenant_resource_source_id
			AND source.tenant_resource_id = resource.id
		JOIN resource_target_revision_v2 target
			ON target.id = NEW.target_revision_id
			AND target.tenant_resource_source_id = source.id
		JOIN resource_allocation_item item
			ON item.id = NEW.allocation_item_id AND item.id = source.allocation_item_id
		JOIN resource_allocation allocation
			ON allocation.id = NEW.allocation_id AND allocation.id = item.allocation_id
		JOIN tenant_access_grant grant
			ON grant.id = NEW.grant_id AND grant.tenant_resource_id = resource.id
			AND grant.tenant_id = resource.tenant_id AND grant.revision = NEW.grant_revision
		JOIN tenant_membership membership
			ON membership.id = NEW.tenant_membership_id
			AND membership.tenant_id = resource.tenant_id AND membership.user_id = NEW.user_id
			AND membership.enabled = 1
			AND (membership.expires_at IS NULL OR membership.expires_at > NEW.started_at)
		JOIN node device
			ON device.id = NEW.device_id AND device.user_id = NEW.user_id AND device.type = 'desktop'
		JOIN technical_resource access_technical
			ON access_technical.id = NEW.access_technical_resource_id
			AND access_technical.id = target.access_technical_resource_id
		WHERE resource.id = NEW.tenant_resource_id AND resource.tenant_id = NEW.tenant_id
			AND allocation.tenant_id = resource.tenant_id
			AND allocation.state = 'active' AND allocation.valid_from <= NEW.started_at
			AND (allocation.expires_at IS NULL OR allocation.expires_at > NEW.started_at)
			AND resource.visibility_state = 'visible' AND source.enabled = 1
			AND grant.status = 'enabled' AND grant.valid_from <= NEW.started_at
			AND (grant.expires_at IS NULL OR grant.expires_at > NEW.started_at)
			AND NEW.user_id = NEW.effective_user_id
			AND ((resource.type = 'container_service' AND target.target_type = 'service_port'
				AND NEW.session_type = 'container_service' AND NEW.action = 'connect')
				OR (resource.type = 'container_ssh' AND target.target_type = 'container'
				AND NEW.session_type = 'container_ssh' AND NEW.action = 'shell'))
			AND ((grant.subject_type = 'user' AND grant.subject_user_id = NEW.user_id)
				OR (grant.subject_type = 'group' AND EXISTS (
					SELECT 1 FROM group_member
					WHERE group_id = grant.subject_group_id AND user_id = NEW.user_id
				)))
	) THEN RAISE(ABORT, 'S4_RESOURCE_SESSION_CHAIN_MISMATCH') END;
	SELECT CASE WHEN NOT EXISTS (SELECT 1 FROM user WHERE id = NEW.actor_user_id)
		OR NOT EXISTS (SELECT 1 FROM user WHERE id = NEW.effective_user_id)
		THEN RAISE(ABORT, 'S4_RESOURCE_SESSION_USER_NOT_FOUND') END;
	SELECT CASE WHEN (NEW.simulation_session_id IS NULL AND NEW.actor_user_id <> NEW.effective_user_id)
		OR (NEW.simulation_session_id IS NOT NULL AND NOT EXISTS (
			SELECT 1 FROM user_simulation_session
			WHERE id = NEW.simulation_session_id
				AND actor_user_id = NEW.actor_user_id AND effective_user_id = NEW.effective_user_id
				AND scope_type = 'tenant' AND scope_id = NEW.tenant_id
		)) THEN RAISE(ABORT, 'S4_RESOURCE_SESSION_SIMULATION_MISMATCH') END;`

const resourceSessionUpdateTriggerBody = `
	SELECT CASE WHEN NEW.tenant_id IS NOT OLD.tenant_id
		OR NEW.tenant_resource_id IS NOT OLD.tenant_resource_id
		OR NEW.tenant_resource_source_id IS NOT OLD.tenant_resource_source_id
		OR NEW.target_revision_id IS NOT OLD.target_revision_id
		OR NEW.allocation_id IS NOT OLD.allocation_id
		OR NEW.allocation_item_id IS NOT OLD.allocation_item_id
		OR NEW.grant_id IS NOT OLD.grant_id OR NEW.grant_revision IS NOT OLD.grant_revision
		OR NEW.user_id IS NOT OLD.user_id OR NEW.tenant_membership_id IS NOT OLD.tenant_membership_id
		OR NEW.device_id IS NOT OLD.device_id OR NEW.actor_user_id IS NOT OLD.actor_user_id
		OR NEW.effective_user_id IS NOT OLD.effective_user_id
		OR NEW.simulation_session_id IS NOT OLD.simulation_session_id
		OR NEW.session_type IS NOT OLD.session_type OR NEW.action IS NOT OLD.action
		OR NEW.access_technical_resource_id IS NOT OLD.access_technical_resource_id
		OR NEW.authorization_revision IS NOT OLD.authorization_revision
		OR NEW.request_id IS NOT OLD.request_id
		OR NEW.started_at IS NOT OLD.started_at
		THEN RAISE(ABORT, 'S4_RESOURCE_SESSION_IDENTITY_IMMUTABLE') END;
	SELECT CASE WHEN NEW.row_version <> OLD.row_version + 1
		THEN RAISE(ABORT, 'S4_RESOURCE_SESSION_VERSION_INVALID') END;
	SELECT CASE WHEN NOT (
		(OLD.status = 'authorizing' AND NEW.status IN ('authorizing','active','ending','rejected'))
		OR (OLD.status = 'active' AND NEW.status IN ('active','ending','ended','terminated'))
		OR (OLD.status = 'ending' AND NEW.status IN ('ending','ended','terminated'))
	) THEN RAISE(ABORT, 'S4_RESOURCE_SESSION_STATUS_TRANSITION_INVALID') END;`

const resourceSessionEventTriggerBody = `
	SELECT CASE WHEN NOT EXISTS (
		SELECT 1 FROM resource_session
		WHERE id = NEW.session_id
			AND access_technical_resource_id = NEW.source_technical_resource_id
	) THEN RAISE(ABORT, 'S4_SESSION_EVENT_SOURCE_MISMATCH') END;`

const resourceSessionTerminationInsertTriggerBody = `
	SELECT CASE WHEN NOT EXISTS (
		SELECT 1 FROM resource_session WHERE id = NEW.session_id
	) THEN RAISE(ABORT, 'S4_SESSION_TERMINATION_SESSION_NOT_FOUND') END;
	SELECT CASE WHEN NEW.command_revision <> COALESCE((
		SELECT MAX(command_revision) + 1 FROM resource_session_termination
		WHERE session_id = NEW.session_id
	), 1) THEN RAISE(ABORT, 'S4_SESSION_TERMINATION_SEQUENCE_INVALID') END;`

const resourceSessionTerminationUpdateTriggerBody = `
	SELECT CASE WHEN NEW.session_id IS NOT OLD.session_id
		OR NEW.command_revision IS NOT OLD.command_revision
		OR NEW.reason_code IS NOT OLD.reason_code OR NEW.reason IS NOT OLD.reason
		OR NEW.created_at IS NOT OLD.created_at
		THEN RAISE(ABORT, 'S4_SESSION_TERMINATION_IDENTITY_IMMUTABLE') END;
	SELECT CASE WHEN NOT (
		(OLD.status = 'pending' AND NEW.status IN ('pending','delivered','acknowledged'))
		OR (OLD.status = 'delivered' AND NEW.status IN ('delivered','acknowledged'))
		OR (OLD.status = 'acknowledged' AND NEW.status = 'acknowledged')
	) THEN RAISE(ABORT, 'S4_SESSION_TERMINATION_STATUS_TRANSITION_INVALID') END;`

func ensureTenantResourceConstraints(database *gorm.DB) error {
	return database.Transaction(func(tx *gorm.DB) error {
		for _, trigger := range tenantResourceTriggers {
			if err := tx.Exec("DROP TRIGGER IF EXISTS " + trigger.name).Error; err != nil {
				return fmt.Errorf("drop Tenant resource constraint trigger %s: %w", trigger.name, err)
			}
			statement := fmt.Sprintf(
				"CREATE TRIGGER IF NOT EXISTS %s BEFORE %s ON %s FOR EACH ROW BEGIN %s END",
				trigger.name,
				trigger.operation,
				trigger.table,
				trigger.body,
			)
			if err := tx.Exec(statement).Error; err != nil {
				return fmt.Errorf("create Tenant resource constraint trigger %s: %w", trigger.name, err)
			}
		}
		return nil
	})
}
