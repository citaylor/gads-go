CREATE TABLE "alert" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "view_id" bigint NOT NULL,
  "user_id" bigint NOT NULL,
  "frequency" integer NOT NULL DEFAULT 0,
  FOREIGN KEY ("user_id") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("view_id") REFERENCES "view"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "alert_idx_user_id" ON "alert" ("user_id");

CREATE INDEX "alert_idx_view_id" ON "alert" ("view_id");

CREATE TABLE "alert_cache" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "layout_id" integer NOT NULL,
  "view_id" bigint NOT NULL,
  "current_id" bigint NOT NULL,
  "user_id" bigint,
  FOREIGN KEY ("current_id") REFERENCES "current"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("user_id") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("view_id") REFERENCES "view"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "alert_cache_idx_current_id" ON "alert_cache" ("current_id");

CREATE INDEX "alert_cache_idx_layout_id" ON "alert_cache" ("layout_id");

CREATE INDEX "alert_cache_idx_user_id" ON "alert_cache" ("user_id");

CREATE INDEX "alert_cache_idx_view_id" ON "alert_cache" ("view_id");

CREATE TABLE "alert_column" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "layout_id" integer NOT NULL,
  "instance_id" integer NOT NULL,
  FOREIGN KEY ("instance_id") REFERENCES "instance"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "alert_column_idx_instance_id" ON "alert_column" ("instance_id");

CREATE INDEX "alert_column_idx_layout_id" ON "alert_column" ("layout_id");

CREATE TABLE "alert_send" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "layout_id" integer,
  "alert_id" integer NOT NULL,
  "current_id" bigint NOT NULL,
  "status" char(7),
  FOREIGN KEY ("alert_id") REFERENCES "alert"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("current_id") REFERENCES "current"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "alert_send_idx_alert_id" ON "alert_send" ("alert_id");

CREATE INDEX "alert_send_idx_current_id" ON "alert_send" ("current_id");

CREATE INDEX "alert_send_idx_layout_id" ON "alert_send" ("layout_id");

CREATE UNIQUE INDEX "alert_send_all" ON "alert_send" ("layout_id", "alert_id", "current_id", "status");

CREATE TABLE "audit" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "site_id" integer,
  "user_id" bigint,
  "type" varchar(45),
  "datetime" datetime,
  "method" varchar(45),
  "url" text,
  "description" text,
  "instance_id" integer,
  FOREIGN KEY ("instance_id") REFERENCES "instance"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("site_id") REFERENCES "site"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("user_id") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "audit_idx_instance_id" ON "audit" ("instance_id");

CREATE INDEX "audit_idx_site_id" ON "audit" ("site_id");

CREATE INDEX "audit_idx_user_id" ON "audit" ("user_id");

CREATE INDEX "audit_idx_datetime" ON "audit" ("datetime");

CREATE INDEX "audit_idx_user_instance_datetime" ON "audit" ("user_id", "instance_id", "datetime");

CREATE TABLE "authentication" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "site_id" integer,
  "type" varchar(32),
  "name" text,
  "xml" text,
  "saml2_firstname" text,
  "saml2_surname" text,
  "enabled" smallint NOT NULL DEFAULT 0,
  "error_messages" text,
  FOREIGN KEY ("site_id") REFERENCES "site"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "authentication_idx_site_id" ON "authentication" ("site_id");

CREATE TABLE "calc" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "layout_id" integer,
  "calc" mediumtext,
  "code" mediumtext,
  "return_format" varchar(45),
  "decimal_places" smallint,
  FOREIGN KEY ("layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "calc_idx_layout_id" ON "calc" ("layout_id");

CREATE TABLE "calc_unique" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "layout_id" integer NOT NULL,
  "value_text" text,
  "value_int" bigint,
  "value_date" date,
  "value_datetime" datetime,
  "value_numeric" decimal(20,5),
  "value_date_from" datetime,
  "value_date_to" datetime,
  FOREIGN KEY ("layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "calc_unique_idx_layout_id" ON "calc_unique" ("layout_id");

CREATE UNIQUE INDEX "calc_unique_ux_layout_date" ON "calc_unique" ("layout_id", "value_date");

CREATE UNIQUE INDEX "calc_unique_ux_layout_daterange" ON "calc_unique" ("layout_id", "value_date_from", "value_date_to");

CREATE UNIQUE INDEX "calc_unique_ux_layout_datetime" ON "calc_unique" ("layout_id", "value_datetime");

CREATE UNIQUE INDEX "calc_unique_ux_layout_int" ON "calc_unique" ("layout_id", "value_int");

CREATE UNIQUE INDEX "calc_unique_ux_layout_numeric" ON "calc_unique" ("layout_id", "value_numeric");

CREATE UNIQUE INDEX "calc_unique_ux_layout_text" ON "calc_unique" ("layout_id", "value_text");

CREATE TABLE "calcval" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "record_id" bigint NOT NULL,
  "layout_id" integer NOT NULL,
  "value_text" text,
  "value_int" bigint,
  "value_date" date,
  "value_datetime" datetime,
  "value_numeric" decimal(20,5),
  "value_date_from" datetime,
  "value_date_to" datetime,
  "purged_by" bigint,
  "purged_on" datetime,
  FOREIGN KEY ("layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("purged_by") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("record_id") REFERENCES "record"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "calcval_idx_layout_id" ON "calcval" ("layout_id");

CREATE INDEX "calcval_idx_purged_by" ON "calcval" ("purged_by");

CREATE INDEX "calcval_idx_record_id" ON "calcval" ("record_id");

CREATE INDEX "calcval_idx_value_text" ON "calcval" ("value_text");

CREATE INDEX "calcval_idx_value_numeric" ON "calcval" ("value_numeric");

CREATE INDEX "calcval_idx_value_int" ON "calcval" ("value_int");

CREATE INDEX "calcval_idx_value_date" ON "calcval" ("value_date");

CREATE INDEX "calcval_idx_value_datetime" ON "calcval" ("value_datetime");

CREATE TABLE "current" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "serial" bigint,
  "current_version_id" bigint,
  "parent_id" bigint,
  "instance_id" integer,
  "linked_id" bigint,
  "deleted" datetime,
  "deletedby" bigint,
  "draftuser_id" bigint,
  FOREIGN KEY ("current_version_id") REFERENCES "record"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("deletedby") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("draftuser_id") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("instance_id") REFERENCES "instance"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("linked_id") REFERENCES "current"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("parent_id") REFERENCES "current"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "current_idx_current_version_id" ON "current" ("current_version_id");

CREATE INDEX "current_idx_deletedby" ON "current" ("deletedby");

CREATE INDEX "current_idx_draftuser_id" ON "current" ("draftuser_id");

CREATE INDEX "current_idx_instance_id" ON "current" ("instance_id");

CREATE INDEX "current_idx_linked_id" ON "current" ("linked_id");

CREATE INDEX "current_idx_parent_id" ON "current" ("parent_id");

CREATE UNIQUE INDEX "current_ux_instance_serial" ON "current" ("instance_id", "serial");

CREATE TABLE "curval" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "record_id" bigint,
  "layout_id" integer,
  "child_unique" smallint NOT NULL DEFAULT 0,
  "value" bigint,
  "purged_by" bigint,
  "purged_on" datetime,
  FOREIGN KEY ("layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("purged_by") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("record_id") REFERENCES "record"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("value") REFERENCES "current"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "curval_idx_layout_id" ON "curval" ("layout_id");

CREATE INDEX "curval_idx_purged_by" ON "curval" ("purged_by");

CREATE INDEX "curval_idx_record_id" ON "curval" ("record_id");

CREATE INDEX "curval_idx_value" ON "curval" ("value");

CREATE TABLE "curval_fields" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "parent_id" integer NOT NULL,
  "child_id" integer NOT NULL,
  FOREIGN KEY ("child_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("parent_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "curval_fields_idx_child_id" ON "curval_fields" ("child_id");

CREATE INDEX "curval_fields_idx_parent_id" ON "curval_fields" ("parent_id");

CREATE TABLE "dashboard" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "site_id" integer,
  "instance_id" integer,
  "user_id" integer,
  FOREIGN KEY ("instance_id") REFERENCES "instance"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("site_id") REFERENCES "site"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("user_id") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "dashboard_idx_instance_id" ON "dashboard" ("instance_id");

CREATE INDEX "dashboard_idx_site_id" ON "dashboard" ("site_id");

CREATE INDEX "dashboard_idx_user_id" ON "dashboard" ("user_id");

CREATE TABLE "date" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "record_id" bigint NOT NULL,
  "layout_id" integer NOT NULL,
  "child_unique" smallint NOT NULL DEFAULT 0,
  "value" date,
  "purged_by" bigint,
  "purged_on" timestamp,
  FOREIGN KEY ("layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("purged_by") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("record_id") REFERENCES "record"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "date_idx_layout_id" ON "date" ("layout_id");

CREATE INDEX "date_idx_purged_by" ON "date" ("purged_by");

CREATE INDEX "date_idx_record_id" ON "date" ("record_id");

CREATE INDEX "date_idx_value" ON "date" ("value");

CREATE TABLE "daterange" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "record_id" bigint NOT NULL,
  "layout_id" integer NOT NULL,
  "from" date,
  "to" date,
  "child_unique" smallint NOT NULL DEFAULT 0,
  "value" varchar(45),
  "purged_by" bigint,
  "purged_on" datetime,
  FOREIGN KEY ("layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("purged_by") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("record_id") REFERENCES "record"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "daterange_idx_layout_id" ON "daterange" ("layout_id");

CREATE INDEX "daterange_idx_purged_by" ON "daterange" ("purged_by");

CREATE INDEX "daterange_idx_record_id" ON "daterange" ("record_id");

CREATE INDEX "daterange_idx_from" ON "daterange" ("from");

CREATE INDEX "daterange_idx_to" ON "daterange" ("to");

CREATE INDEX "daterange_idx_value" ON "daterange" ("value");

CREATE TABLE "department" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "name" varchar(128),
  "site_id" integer,
  "deleted" smallint NOT NULL DEFAULT 0,
  FOREIGN KEY ("site_id") REFERENCES "site"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "department_idx_site_id" ON "department" ("site_id");

CREATE TABLE "display_field" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "layout_id" integer NOT NULL,
  "display_field_id" integer NOT NULL,
  "regex" text,
  "operator" varchar(16),
  FOREIGN KEY ("display_field_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "display_field_idx_display_field_id" ON "display_field" ("display_field_id");

CREATE INDEX "display_field_idx_layout_id" ON "display_field" ("layout_id");

CREATE TABLE "enum" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "record_id" bigint,
  "layout_id" integer,
  "child_unique" smallint NOT NULL DEFAULT 0,
  "value" integer,
  "purged_by" bigint,
  "purged_on" datetime,
  FOREIGN KEY ("layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("purged_by") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("record_id") REFERENCES "record"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("value") REFERENCES "enumval"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "enum_idx_layout_id" ON "enum" ("layout_id");

CREATE INDEX "enum_idx_purged_by" ON "enum" ("purged_by");

CREATE INDEX "enum_idx_record_id" ON "enum" ("record_id");

CREATE INDEX "enum_idx_value" ON "enum" ("value");

CREATE TABLE "enumval" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "value" text,
  "layout_id" integer,
  "deleted" smallint NOT NULL DEFAULT 0,
  "parent" integer,
  "position" integer,
  FOREIGN KEY ("layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("parent") REFERENCES "enumval"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "enumval_idx_layout_id" ON "enumval" ("layout_id");

CREATE INDEX "enumval_idx_parent" ON "enumval" ("parent");

CREATE INDEX "enumval_idx_value" ON "enumval" ("value");

CREATE TABLE "export" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "site_id" integer,
  "user_id" bigint NOT NULL,
  "type" varchar(45),
  "started" datetime,
  "completed" datetime,
  "result" text,
  "result_internal" text,
  "mimetype" text,
  "content" longblob,
  FOREIGN KEY ("site_id") REFERENCES "site"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("user_id") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "export_idx_site_id" ON "export" ("site_id");

CREATE INDEX "export_idx_user_id" ON "export" ("user_id");

CREATE TABLE "file" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "record_id" bigint,
  "layout_id" integer,
  "child_unique" smallint NOT NULL DEFAULT 0,
  "value" bigint,
  "purged_by" bigint,
  "purged_on" datetime,
  FOREIGN KEY ("layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("purged_by") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("record_id") REFERENCES "record"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("value") REFERENCES "fileval"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "file_idx_layout_id" ON "file" ("layout_id");

CREATE INDEX "file_idx_purged_by" ON "file" ("purged_by");

CREATE INDEX "file_idx_record_id" ON "file" ("record_id");

CREATE INDEX "file_idx_value" ON "file" ("value");

CREATE TABLE "file_option" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "layout_id" integer NOT NULL,
  "filesize" integer,
  FOREIGN KEY ("layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "file_option_idx_layout_id" ON "file_option" ("layout_id");

CREATE TABLE "fileval" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "name" text,
  "mimetype" text,
  "is_independent" smallint NOT NULL DEFAULT 0,
  "edit_user_id" bigint,
  FOREIGN KEY ("edit_user_id") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "fileval_idx_edit_user_id" ON "fileval" ("edit_user_id");

CREATE INDEX "fileval_idx_name" ON "fileval" ("name");

CREATE TABLE "filter" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "view_id" bigint NOT NULL,
  "layout_id" integer NOT NULL,
  FOREIGN KEY ("layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("view_id") REFERENCES "view"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "filter_idx_layout_id" ON "filter" ("layout_id");

CREATE INDEX "filter_idx_view_id" ON "filter" ("view_id");

CREATE TABLE "filtered_value" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "submission_id" integer,
  "layout_id" integer,
  "current_id" integer,
  FOREIGN KEY ("current_id") REFERENCES "current"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("submission_id") REFERENCES "submission"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "filtered_value_idx_current_id" ON "filtered_value" ("current_id");

CREATE INDEX "filtered_value_idx_layout_id" ON "filtered_value" ("layout_id");

CREATE INDEX "filtered_value_idx_submission_id" ON "filtered_value" ("submission_id");

CREATE UNIQUE INDEX "ux_submission_layout_current" ON "filtered_value" ("submission_id", "layout_id", "current_id");

CREATE TABLE "graph" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "title" text,
  "description" text,
  "y_axis" integer,
  "y_axis_link" integer,
  "y_axis_stack" varchar(45),
  "y_axis_label" text,
  "x_axis" integer,
  "x_axis_link" integer,
  "x_axis_grouping" varchar(45),
  "group_by" integer,
  "stackseries" smallint NOT NULL DEFAULT 0,
  "as_percent" smallint NOT NULL DEFAULT 0,
  "type" varchar(45),
  "metric_group" integer,
  "instance_id" integer,
  "is_shared" smallint NOT NULL DEFAULT 0,
  "user_id" bigint,
  "group_id" integer,
  "trend" varchar(45),
  "from" date,
  "to" date,
  "x_axis_range" varchar(45),
  FOREIGN KEY ("group_id") REFERENCES "group"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("group_by") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("instance_id") REFERENCES "instance"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("metric_group") REFERENCES "metric_group"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("user_id") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("x_axis") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("x_axis_link") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("y_axis") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("y_axis_link") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "graph_idx_group_id" ON "graph" ("group_id");

CREATE INDEX "graph_idx_group_by" ON "graph" ("group_by");

CREATE INDEX "graph_idx_instance_id" ON "graph" ("instance_id");

CREATE INDEX "graph_idx_metric_group" ON "graph" ("metric_group");

CREATE INDEX "graph_idx_user_id" ON "graph" ("user_id");

CREATE INDEX "graph_idx_x_axis" ON "graph" ("x_axis");

CREATE INDEX "graph_idx_x_axis_link" ON "graph" ("x_axis_link");

CREATE INDEX "graph_idx_y_axis" ON "graph" ("y_axis");

CREATE INDEX "graph_idx_y_axis_link" ON "graph" ("y_axis_link");

CREATE TABLE "graph_color" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "name" varchar(128),
  "color" char(6)
);

CREATE UNIQUE INDEX "ux_graph_color_name" ON "graph_color" ("name");

CREATE TABLE "group" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "name" varchar(128),
  "default_read" smallint NOT NULL DEFAULT 0,
  "default_write_new" smallint NOT NULL DEFAULT 0,
  "default_write_existing" smallint NOT NULL DEFAULT 0,
  "default_approve_new" smallint NOT NULL DEFAULT 0,
  "default_approve_existing" smallint NOT NULL DEFAULT 0,
  "default_write_new_no_approval" smallint NOT NULL DEFAULT 0,
  "default_write_existing_no_approval" smallint NOT NULL DEFAULT 0,
  "site_id" integer,
  FOREIGN KEY ("site_id") REFERENCES "site"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "group_idx_site_id" ON "group" ("site_id");

CREATE TABLE "import" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "site_id" integer,
  "instance_id" integer,
  "user_id" bigint NOT NULL,
  "type" varchar(45),
  "row_count" integer NOT NULL DEFAULT 0,
  "started" datetime,
  "completed" datetime,
  "written_count" integer NOT NULL DEFAULT 0,
  "error_count" integer NOT NULL DEFAULT 0,
  "skipped_count" integer NOT NULL DEFAULT 0,
  "result" text,
  FOREIGN KEY ("instance_id") REFERENCES "instance"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("site_id") REFERENCES "site"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("user_id") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "import_idx_instance_id" ON "import" ("instance_id");

CREATE INDEX "import_idx_site_id" ON "import" ("site_id");

CREATE INDEX "import_idx_user_id" ON "import" ("user_id");

CREATE TABLE "import_row" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "import_id" int NOT NULL,
  "status" varchar(45),
  "content" text,
  "errors" text,
  "changes" text,
  FOREIGN KEY ("import_id") REFERENCES "import"("id") ON DELETE CASCADE ON UPDATE NO ACTION
);

CREATE INDEX "import_row_idx_import_id" ON "import_row" ("import_id");

CREATE TABLE "instance" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "name" text,
  "name_short" varchar(64),
  "site_id" integer,
  "sort_layout_id" integer,
  "sort_type" varchar(45),
  "view_limit_id" integer,
  "default_view_limit_extra_id" integer,
  "homepage_text" text,
  "homepage_text2" text,
  "record_name" text,
  "forget_history" smallint DEFAULT 0,
  "no_overnight_update" smallint DEFAULT 0,
  "api_index_layout_id" integer,
  "forward_record_after_create" smallint DEFAULT 0,
  "no_hide_blank" smallint NOT NULL DEFAULT 0,
  "no_download_pdf" smallint NOT NULL DEFAULT 0,
  "no_copy_record" smallint NOT NULL DEFAULT 0,
  "hide_in_selector" smallint NOT NULL DEFAULT 0,
  "security_marking" text,
  FOREIGN KEY ("api_index_layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("default_view_limit_extra_id") REFERENCES "view"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("site_id") REFERENCES "site"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("sort_layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("view_limit_id") REFERENCES "view"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "instance_idx_api_index_layout_id" ON "instance" ("api_index_layout_id");

CREATE INDEX "instance_idx_default_view_limit_extra_id" ON "instance" ("default_view_limit_extra_id");

CREATE INDEX "instance_idx_site_id" ON "instance" ("site_id");

CREATE INDEX "instance_idx_sort_layout_id" ON "instance" ("sort_layout_id");

CREATE INDEX "instance_idx_view_limit_id" ON "instance" ("view_limit_id");

CREATE TABLE "instance_group" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "instance_id" integer NOT NULL,
  "group_id" integer NOT NULL,
  "permission" varchar(45) NOT NULL,
  FOREIGN KEY ("group_id") REFERENCES "group"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("instance_id") REFERENCES "instance"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "instance_group_idx_group_id" ON "instance_group" ("group_id");

CREATE INDEX "instance_group_idx_instance_id" ON "instance_group" ("instance_id");

CREATE UNIQUE INDEX "instance_group_ux_instance_group_permission" ON "instance_group" ("instance_id", "group_id", "permission");

CREATE TABLE "instance_rag" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "instance_id" integer NOT NULL,
  "rag" varchar(16) NOT NULL,
  "enabled" smallint NOT NULL DEFAULT 0,
  "description" text,
  FOREIGN KEY ("instance_id") REFERENCES "instance"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "instance_rag_idx_instance_id" ON "instance_rag" ("instance_id");

CREATE UNIQUE INDEX "instance_rag_ux_instance_rag" ON "instance_rag" ("instance_id", "rag");

CREATE TABLE "intgr" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "record_id" bigint NOT NULL,
  "layout_id" integer NOT NULL,
  "child_unique" smallint NOT NULL DEFAULT 0,
  "value" bigint,
  "purged_by" bigint,
  "purged_on" datetime,
  FOREIGN KEY ("layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("purged_by") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("record_id") REFERENCES "record"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "intgr_idx_layout_id" ON "intgr" ("layout_id");

CREATE INDEX "intgr_idx_purged_by" ON "intgr" ("purged_by");

CREATE INDEX "intgr_idx_record_id" ON "intgr" ("record_id");

CREATE INDEX "intgr_idx_value" ON "intgr" ("value");

CREATE TABLE "layout" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "name" text,
  "name_short" varchar(64),
  "type" varchar(45),
  "permission" integer NOT NULL DEFAULT 0,
  "optional" smallint NOT NULL DEFAULT 0,
  "remember" smallint NOT NULL DEFAULT 0,
  "isunique" smallint NOT NULL DEFAULT 0,
  "textbox" smallint NOT NULL DEFAULT 0,
  "typeahead" smallint NOT NULL DEFAULT 0,
  "force_regex" text,
  "position" integer,
  "ordering" varchar(45),
  "end_node_only" smallint NOT NULL DEFAULT 0,
  "multivalue" smallint NOT NULL DEFAULT 0,
  "can_child" smallint NOT NULL DEFAULT 0,
  "internal" smallint NOT NULL DEFAULT 0,
  "description" text,
  "helptext" text,
  "options" text,
  "display_field" integer,
  "display_regex" text,
  "display_condition" char(3),
  "display_matchtype" text,
  "instance_id" integer,
  "link_parent" integer,
  "related_field" integer,
  "width" integer NOT NULL DEFAULT 50,
  "filter" text,
  "topic_id" integer,
  "aggregate" varchar(45),
  "group_display" varchar(45),
  "lookup_endpoint" text,
  "lookup_group" smallint,
  "notes" text,
  FOREIGN KEY ("display_field") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("instance_id") REFERENCES "instance"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("link_parent") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("related_field") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("topic_id") REFERENCES "topic"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "layout_idx_display_field" ON "layout" ("display_field");

CREATE INDEX "layout_idx_instance_id" ON "layout" ("instance_id");

CREATE INDEX "layout_idx_link_parent" ON "layout" ("link_parent");

CREATE INDEX "layout_idx_related_field" ON "layout" ("related_field");

CREATE INDEX "layout_idx_topic_id" ON "layout" ("topic_id");

CREATE UNIQUE INDEX "layout_ux_instance_name_short" ON "layout" ("instance_id", "name_short");

CREATE TABLE "layout_depend" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "layout_id" integer NOT NULL,
  "depends_on" integer NOT NULL,
  FOREIGN KEY ("depends_on") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "layout_depend_idx_depends_on" ON "layout_depend" ("depends_on");

CREATE INDEX "layout_depend_idx_layout_id" ON "layout_depend" ("layout_id");

CREATE TABLE "layout_group" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "layout_id" integer NOT NULL,
  "group_id" integer NOT NULL,
  "permission" varchar(45) NOT NULL,
  FOREIGN KEY ("group_id") REFERENCES "group"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "layout_group_idx_group_id" ON "layout_group" ("group_id");

CREATE INDEX "layout_group_idx_layout_id" ON "layout_group" ("layout_id");

CREATE INDEX "layout_group_idx_permission" ON "layout_group" ("permission");

CREATE UNIQUE INDEX "layout_group_ux_layout_group_permission" ON "layout_group" ("layout_id", "group_id", "permission");

CREATE TABLE "metric" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "metric_group" integer NOT NULL,
  "x_axis_value" text,
  "target" bigint,
  "y_axis_grouping_value" text,
  FOREIGN KEY ("metric_group") REFERENCES "metric_group"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "metric_idx_metric_group" ON "metric" ("metric_group");

CREATE TABLE "metric_group" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "name" text,
  "instance_id" integer,
  FOREIGN KEY ("instance_id") REFERENCES "instance"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "metric_group_idx_instance_id" ON "metric_group" ("instance_id");

CREATE TABLE "oauthclient" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "client_id" varchar(64) NOT NULL,
  "client_secret" varchar(64) NOT NULL
);

CREATE TABLE "oauthtoken" (
  "token" varchar(128) NOT NULL,
  "related_token" varchar(128) NOT NULL,
  "oauthclient_id" integer NOT NULL,
  "user_id" bigint NOT NULL,
  "type" varchar(12) NOT NULL,
  "expires" integer,
  PRIMARY KEY ("token"),
  FOREIGN KEY ("oauthclient_id") REFERENCES "oauthclient"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("user_id") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "oauthtoken_idx_oauthclient_id" ON "oauthtoken" ("oauthclient_id");

CREATE INDEX "oauthtoken_idx_user_id" ON "oauthtoken" ("user_id");

CREATE TABLE "organisation" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "name" varchar(128),
  "site_id" integer,
  "deleted" smallint NOT NULL DEFAULT 0,
  FOREIGN KEY ("site_id") REFERENCES "site"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "organisation_idx_site_id" ON "organisation" ("site_id");

CREATE TABLE "permission" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "name" varchar(128) NOT NULL,
  "description" text,
  "order" integer
);

CREATE TABLE "person" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "record_id" bigint,
  "layout_id" integer,
  "child_unique" smallint NOT NULL DEFAULT 0,
  "value" bigint,
  "purged_by" bigint,
  "purged_on" datetime,
  FOREIGN KEY ("layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("purged_by") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("record_id") REFERENCES "record"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("value") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "person_idx_layout_id" ON "person" ("layout_id");

CREATE INDEX "person_idx_purged_by" ON "person" ("purged_by");

CREATE INDEX "person_idx_record_id" ON "person" ("record_id");

CREATE INDEX "person_idx_value" ON "person" ("value");

CREATE TABLE "rag" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "layout_id" integer NOT NULL,
  "red" text,
  "amber" text,
  "green" text,
  "code" mediumtext,
  FOREIGN KEY ("layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "rag_idx_layout_id" ON "rag" ("layout_id");

CREATE TABLE "ragval" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "record_id" bigint NOT NULL,
  "layout_id" integer NOT NULL,
  "value" varchar(16),
  "purged_by" bigint,
  "purged_on" datetime,
  FOREIGN KEY ("layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("purged_by") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("record_id") REFERENCES "record"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "ragval_idx_layout_id" ON "ragval" ("layout_id");

CREATE INDEX "ragval_idx_purged_by" ON "ragval" ("purged_by");

CREATE INDEX "ragval_idx_record_id" ON "ragval" ("record_id");

CREATE INDEX "ragval_idx_value" ON "ragval" ("value");

CREATE UNIQUE INDEX "ragval_ux_record_layout" ON "ragval" ("record_id", "layout_id");

CREATE TABLE "record" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "created" datetime NOT NULL,
  "current_id" bigint NOT NULL DEFAULT 0,
  "createdby" bigint,
  "approvedby" bigint,
  "record_id" bigint,
  "approval" smallint NOT NULL DEFAULT 0,
  FOREIGN KEY ("approvedby") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("createdby") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("current_id") REFERENCES "current"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("record_id") REFERENCES "record"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "record_idx_approvedby" ON "record" ("approvedby");

CREATE INDEX "record_idx_createdby" ON "record" ("createdby");

CREATE INDEX "record_idx_current_id" ON "record" ("current_id");

CREATE INDEX "record_idx_current_id_approval_created" ON "record" ("current_id", "approval", "created", "id");

CREATE INDEX "record_idx_record_id" ON "record" ("record_id");

CREATE INDEX "record_idx_approval" ON "record" ("approval");

CREATE TABLE "report" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "name" text NOT NULL,
  "title" text,
  "description" text,
  "user_id" bigint,
  "createdby" bigint,
  "created" datetime,
  "instance_id" bigint,
  "deleted" datetime,
  "security_marking" text,
  FOREIGN KEY ("createdby") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("instance_id") REFERENCES "instance"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("user_id") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "report_idx_createdby" ON "report" ("createdby");

CREATE INDEX "report_idx_instance_id" ON "report" ("instance_id");

CREATE INDEX "report_idx_user_id" ON "report" ("user_id");

CREATE TABLE "report_group" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "report_id" integer NOT NULL,
  "group_id" integer NOT NULL,
  FOREIGN KEY ("group_id") REFERENCES "group"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("report_id") REFERENCES "report"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "report_group_idx_group_id" ON "report_group" ("group_id");

CREATE INDEX "report_group_idx_report_id" ON "report_group" ("report_id");

CREATE TABLE "report_layout" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "report_id" integer NOT NULL,
  "layout_id" bigint NOT NULL,
  "order" integer,
  FOREIGN KEY ("layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("report_id") REFERENCES "report"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "report_layout_idx_layout_id" ON "report_layout" ("layout_id");

CREATE INDEX "report_layout_idx_report_id" ON "report_layout" ("report_id");

CREATE TABLE "site" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "host" varchar(128),
  "name" text,
  "created" datetime,
  "email_welcome_text" text,
  "email_welcome_subject" text,
  "email_delete_text" text,
  "email_delete_subject" text,
  "email_reject_text" text,
  "email_reject_subject" text,
  "register_text" text,
  "homepage_text" text,
  "homepage_text2" text,
  "register_title_help" text,
  "register_freetext1_help" text,
  "register_freetext2_help" text,
  "register_email_help" text,
  "register_organisation_help" text,
  "register_organisation_name" text,
  "register_organisation_mandatory" smallint NOT NULL DEFAULT 0,
  "register_department_help" text,
  "register_department_name" text,
  "register_department_mandatory" smallint NOT NULL DEFAULT 0,
  "register_team_help" text,
  "register_team_name" text,
  "register_team_mandatory" smallint NOT NULL DEFAULT 0,
  "register_notes_help" text,
  "register_freetext1_name" text,
  "register_freetext2_name" text,
  "register_show_organisation" smallint NOT NULL DEFAULT 1,
  "register_show_department" smallint NOT NULL DEFAULT 0,
  "register_show_team" smallint NOT NULL DEFAULT 0,
  "register_show_title" smallint NOT NULL DEFAULT 1,
  "hide_account_request" smallint NOT NULL DEFAULT 0,
  "remember_user_location" smallint NOT NULL DEFAULT 1,
  "user_editable_fields" text,
  "register_freetext1_placeholder" text,
  "register_freetext2_placeholder" text,
  "account_request_notes_name" text,
  "account_request_notes_placeholder" text,
  "security_marking" text,
  "site_logo" longblob,
  "force_mfa" char(3)
);

CREATE TABLE "sort" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "view_id" bigint NOT NULL,
  "layout_id" integer,
  "parent_id" integer,
  "type" varchar(45),
  "order" integer,
  FOREIGN KEY ("layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("parent_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("view_id") REFERENCES "view"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "sort_idx_layout_id" ON "sort" ("layout_id");

CREATE INDEX "sort_idx_parent_id" ON "sort" ("parent_id");

CREATE INDEX "sort_idx_view_id" ON "sort" ("view_id");

CREATE TABLE "string" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "record_id" bigint NOT NULL,
  "layout_id" integer NOT NULL,
  "child_unique" smallint NOT NULL DEFAULT 0,
  "value" text,
  "value_index" varchar(128),
  "purged_by" bigint,
  "purged_on" datetime,
  FOREIGN KEY ("layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("purged_by") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("record_id") REFERENCES "record"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "string_idx_layout_id" ON "string" ("layout_id");

CREATE INDEX "string_idx_purged_by" ON "string" ("purged_by");

CREATE INDEX "string_idx_record_id" ON "string" ("record_id");

CREATE INDEX "string_idx_value_index" ON "string" ("value_index");

CREATE TABLE "submission" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "token" varchar(64) NOT NULL,
  "created" datetime,
  "submitted" smallint NOT NULL DEFAULT 0
);

CREATE UNIQUE INDEX "ux_submission_token" ON "submission" ("token", "submitted");

CREATE TABLE "team" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "name" varchar(128),
  "site_id" integer,
  "deleted" smallint NOT NULL DEFAULT 0,
  FOREIGN KEY ("site_id") REFERENCES "site"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "team_idx_site_id" ON "team" ("site_id");

CREATE TABLE "title" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "name" varchar(128),
  "site_id" integer,
  "deleted" smallint NOT NULL DEFAULT 0,
  FOREIGN KEY ("site_id") REFERENCES "site"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "title_idx_site_id" ON "title" ("site_id");

CREATE TABLE "topic" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "instance_id" integer,
  "name" text,
  "description" text,
  "initial_state" varchar(32),
  "click_to_edit" smallint NOT NULL DEFAULT 0,
  "prevent_edit_topic_id" integer,
  FOREIGN KEY ("instance_id") REFERENCES "instance"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("prevent_edit_topic_id") REFERENCES "topic"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "topic_idx_instance_id" ON "topic" ("instance_id");

CREATE INDEX "topic_idx_prevent_edit_topic_id" ON "topic" ("prevent_edit_topic_id");

CREATE TABLE "user" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "site_id" integer,
  "firstname" varchar(128),
  "surname" varchar(128),
  "email" text,
  "username" text,
  "title" integer,
  "organisation" integer,
  "department_id" integer,
  "team_id" integer,
  "freetext1" text,
  "freetext2" text,
  "password" varchar(128),
  "pwchanged" datetime,
  "resetpw" varchar(32),
  "deleted" datetime,
  "lastlogin" datetime,
  "lastfail" datetime,
  "failcount" integer NOT NULL DEFAULT 0,
  "lastrecord" bigint,
  "lastview" bigint,
  "session_settings" text,
  "value" text,
  "account_request" smallint DEFAULT 0,
  "account_request_notes" text,
  "aup_accepted" datetime,
  "limit_to_view" bigint,
  "stylesheet" text,
  "created" datetime,
  "created_by_id" bigint,
  "debug_login" smallint DEFAULT 0,
  "mfa_type" char(3),
  "mobile" text,
  "mobile_verified" smallint NOT NULL DEFAULT 0,
  "mfa_secret" text,
  "mfa_sms_token" text,
  "mfa_sms_created" datetime,
  "mfa_token_previous" text,
  "mfa_token_previous_type" char(3),
  "mfa_token_previous_used" datetime,
  "mfa_token_previous_key" text,
  "mfa_lastfail" datetime,
  "mfa_failcount" integer NOT NULL DEFAULT 0,
  FOREIGN KEY ("created_by_id") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("department_id") REFERENCES "department"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("lastrecord") REFERENCES "record"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("lastview") REFERENCES "view"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("limit_to_view") REFERENCES "view"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("organisation") REFERENCES "organisation"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("site_id") REFERENCES "site"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("team_id") REFERENCES "team"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("title") REFERENCES "title"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "user_idx_created_by_id" ON "user" ("created_by_id");

CREATE INDEX "user_idx_department_id" ON "user" ("department_id");

CREATE INDEX "user_idx_lastrecord" ON "user" ("lastrecord");

CREATE INDEX "user_idx_lastview" ON "user" ("lastview");

CREATE INDEX "user_idx_limit_to_view" ON "user" ("limit_to_view");

CREATE INDEX "user_idx_organisation" ON "user" ("organisation");

CREATE INDEX "user_idx_site_id" ON "user" ("site_id");

CREATE INDEX "user_idx_team_id" ON "user" ("team_id");

CREATE INDEX "user_idx_title" ON "user" ("title");

CREATE INDEX "user_idx_value" ON "user" ("value");

CREATE INDEX "user_idx_email" ON "user" ("email");

CREATE INDEX "user_idx_username" ON "user" ("username");

CREATE TABLE "user_graph" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "user_id" bigint NOT NULL,
  "graph_id" integer NOT NULL,
  FOREIGN KEY ("graph_id") REFERENCES "graph"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("user_id") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "user_graph_idx_graph_id" ON "user_graph" ("graph_id");

CREATE INDEX "user_graph_idx_user_id" ON "user_graph" ("user_id");

CREATE TABLE "user_group" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "user_id" bigint NOT NULL,
  "group_id" integer NOT NULL,
  FOREIGN KEY ("group_id") REFERENCES "group"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("user_id") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "user_group_idx_group_id" ON "user_group" ("group_id");

CREATE INDEX "user_group_idx_user_id" ON "user_group" ("user_id");

CREATE TABLE "user_lastrecord" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "record_id" bigint NOT NULL,
  "instance_id" integer NOT NULL,
  "user_id" bigint NOT NULL,
  FOREIGN KEY ("instance_id") REFERENCES "instance"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("record_id") REFERENCES "record"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("user_id") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "user_lastrecord_idx_instance_id" ON "user_lastrecord" ("instance_id");

CREATE INDEX "user_lastrecord_idx_record_id" ON "user_lastrecord" ("record_id");

CREATE INDEX "user_lastrecord_idx_user_id" ON "user_lastrecord" ("user_id");

CREATE TABLE "user_permission" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "user_id" bigint NOT NULL,
  "permission_id" integer NOT NULL,
  FOREIGN KEY ("permission_id") REFERENCES "permission"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("user_id") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "user_permission_idx_permission_id" ON "user_permission" ("permission_id");

CREATE INDEX "user_permission_idx_user_id" ON "user_permission" ("user_id");

CREATE TABLE "view" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "user_id" bigint,
  "group_id" integer,
  "name" varchar(128),
  "global" smallint NOT NULL DEFAULT 0,
  "is_admin" smallint NOT NULL DEFAULT 0,
  "is_limit_extra" smallint NOT NULL DEFAULT 0,
  "is_limit_override" smallint NOT NULL DEFAULT 0,
  "filter" text,
  "instance_id" integer,
  "created" datetime,
  "createdby" bigint,
  FOREIGN KEY ("createdby") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("group_id") REFERENCES "group"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("instance_id") REFERENCES "instance"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("user_id") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "view_idx_createdby" ON "view" ("createdby");

CREATE INDEX "view_idx_group_id" ON "view" ("group_id");

CREATE INDEX "view_idx_instance_id" ON "view" ("instance_id");

CREATE INDEX "view_idx_user_id" ON "view" ("user_id");

CREATE TABLE "view_group" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "view_id" bigint NOT NULL,
  "layout_id" integer,
  "parent_id" integer,
  "order" integer,
  FOREIGN KEY ("layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("parent_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("view_id") REFERENCES "view"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "view_group_idx_layout_id" ON "view_group" ("layout_id");

CREATE INDEX "view_group_idx_parent_id" ON "view_group" ("parent_id");

CREATE INDEX "view_group_idx_view_id" ON "view_group" ("view_id");

CREATE TABLE "view_layout" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "view_id" bigint NOT NULL,
  "layout_id" integer NOT NULL,
  "order" integer,
  FOREIGN KEY ("layout_id") REFERENCES "layout"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("view_id") REFERENCES "view"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "view_layout_idx_layout_id" ON "view_layout" ("layout_id");

CREATE INDEX "view_layout_idx_view_id" ON "view_layout" ("view_id");

CREATE TABLE "view_limit" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "view_id" bigint NOT NULL,
  "user_id" bigint NOT NULL,
  FOREIGN KEY ("user_id") REFERENCES "user"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("view_id") REFERENCES "view"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "view_limit_idx_user_id" ON "view_limit" ("user_id");

CREATE INDEX "view_limit_idx_view_id" ON "view_limit" ("view_id");

CREATE TABLE "widget" (
  "id" INTEGER PRIMARY KEY NOT NULL,
  "grid_id" varchar(64),
  "dashboard_id" integer,
  "type" varchar(16),
  "title" text,
  "static" smallint NOT NULL DEFAULT 0,
  "h" smallint DEFAULT 0,
  "w" smallint DEFAULT 0,
  "x" smallint DEFAULT 0,
  "y" smallint DEFAULT 0,
  "content" text,
  "view_id" integer,
  "graph_id" integer,
  "rows" integer,
  "tl_options" text,
  "globe_options" text,
  FOREIGN KEY ("dashboard_id") REFERENCES "dashboard"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("graph_id") REFERENCES "graph"("id") ON DELETE NO ACTION ON UPDATE NO ACTION,
  FOREIGN KEY ("view_id") REFERENCES "view"("id") ON DELETE NO ACTION ON UPDATE NO ACTION
);

CREATE INDEX "widget_idx_dashboard_id" ON "widget" ("dashboard_id");

CREATE INDEX "widget_idx_graph_id" ON "widget" ("graph_id");

CREATE INDEX "widget_idx_view_id" ON "widget" ("view_id");

CREATE UNIQUE INDEX "widget_ux_dashboard_grid" ON "widget" ("dashboard_id", "grid_id");
