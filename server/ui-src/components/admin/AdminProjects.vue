<script>
import CommonMixins from "../../mixins/CommonMixins";

export default {
	mixins: [CommonMixins],

	props: {
		projects: {
			type: Array,
			required: true,
		},
	},

	emits: ["changed"],

	data() {
		return {
			form: { name: "", slug: "" },
			editing: { ID: "", Name: "", Slug: "" },
			deleting: { ID: "", Name: "", Slug: "" },
			confirmSlug: "",
			created: null, // project returned with a newly generated API key
			smtpHost: location.hostname,
		};
	},

	methods: {
		create() {
			this.post(this.resolve("/api/v1/admin/projects"), { Name: this.form.name, Slug: this.form.slug }, (r) => {
				this.form = { name: "", slug: "" };
				this.showKey(r.data);
			});
		},

		showKey(project) {
			this.created = project;
			this.$emit("changed");
			this.modal("ApiKeyModal").show();
		},

		toggle(p) {
			this.put(this.resolve(`/api/v1/admin/projects/${p.ID}`), { Disabled: !p.Disabled }, () => {
				this.$emit("changed");
			});
		},

		rotate(p) {
			if (
				!window.confirm(
					`Generate a new API key for "${p.Name}"?\n\nThe current key stops working immediately, ` +
						"so every application using it must be updated.",
				)
			) {
				return;
			}

			this.post(this.resolve(`/api/v1/admin/projects/${p.ID}/api-key`), {}, (r) => {
				this.showKey(r.data);
			});
		},

		edit(p) {
			this.editing = { ID: p.ID, Name: p.Name, Slug: p.Slug };
			this.modal("EditProjectModal").show();
		},

		saveEdit() {
			this.put(
				this.resolve(`/api/v1/admin/projects/${this.editing.ID}`),
				{ Name: this.editing.Name, Slug: this.editing.Slug },
				() => {
					this.modal("EditProjectModal").hide();
					this.$emit("changed");
				},
			);
		},

		askDelete(p) {
			this.deleting = { ID: p.ID, Name: p.Name, Slug: p.Slug };
			this.confirmSlug = "";
			this.modal("DeleteProjectModal").show();
		},

		remove() {
			this.delete(this.resolve(`/api/v1/admin/projects/${this.deleting.ID}`), false, () => {
				this.modal("DeleteProjectModal").hide();
				this.$emit("changed");
			});
		},
	},
};
</script>

<template>
	<div>
		<form class="row g-2 align-items-end mb-4" data-testid="project-form" @submit.prevent="create">
			<div class="col-12 col-md-4">
				<label for="project-name" class="form-label mb-1">Project name</label>
				<input id="project-name" v-model="form.name" class="form-control" required maxlength="100" />
			</div>
			<div class="col-12 col-md-4">
				<label for="project-slug" class="form-label mb-1"
					>Slug <span class="text-muted">(optional)</span></label
				>
				<input
					id="project-slug"
					v-model="form.slug"
					class="form-control"
					pattern="[a-z0-9][a-z0-9\-]{0,62}"
					placeholder="generated from the name"
					title="Lowercase letters, numbers and hyphens"
				/>
			</div>
			<div class="col-12 col-md-auto">
				<button type="submit" class="btn btn-primary"><i class="bi bi-plus-lg me-1"></i>Create project</button>
			</div>
		</form>

		<div class="table-responsive">
			<table class="table align-middle" data-testid="projects-table">
				<thead>
					<tr>
						<th>Project</th>
						<th>Messages</th>
						<th>API key</th>
						<th>Status</th>
						<th class="text-end">Actions</th>
					</tr>
				</thead>
				<tbody>
					<tr v-for="p in projects" :key="p.ID" :data-testid="'project-' + p.Slug">
						<td>
							<div class="fw-bold">{{ p.Name }}</div>
							<code class="small">{{ p.Slug }}</code>
						</td>
						<td>
							{{ formatNumber(p.Messages || 0) }}
							<span v-if="p.Unread" class="badge rounded-pill text-bg-secondary ms-1"
								>{{ formatNumber(p.Unread) }} unread</span
							>
						</td>
						<td>
							<code>mpk_••••••••{{ p.KeyHint }}</code>
						</td>
						<td>
							<span v-if="p.Disabled" class="badge text-bg-warning">Disabled</span>
							<span v-else class="badge text-bg-success">Enabled</span>
						</td>
						<td class="text-end text-nowrap">
							<button
								class="btn btn-sm me-1"
								:class="p.Disabled ? 'btn-outline-success' : 'btn-outline-warning'"
								:title="p.Disabled ? 'Enable the project' : 'Disable the API key and incoming messages'"
								data-testid="toggle-project"
								@click="toggle(p)"
							>
								<i class="bi" :class="p.Disabled ? 'bi-play-fill' : 'bi-pause-fill'"></i>
								{{ p.Disabled ? "Enable" : "Disable" }}
							</button>
							<button
								class="btn btn-sm btn-outline-secondary me-1"
								title="Generate a new API key"
								@click="rotate(p)"
							>
								<i class="bi bi-key-fill"></i>
							</button>
							<button class="btn btn-sm btn-outline-secondary me-1" title="Edit" @click="edit(p)">
								<i class="bi bi-pencil-fill"></i>
							</button>
							<button class="btn btn-sm btn-outline-danger" title="Delete" @click="askDelete(p)">
								<i class="bi bi-trash-fill"></i>
							</button>
						</td>
					</tr>
					<tr v-if="!projects.length">
						<td colspan="5" class="text-center text-muted py-4">
							No projects yet, create the first one above.
						</td>
					</tr>
				</tbody>
			</table>
		</div>

		<div id="ApiKeyModal" class="modal fade" tabindex="-1" aria-labelledby="ApiKeyModalLabel" aria-hidden="true">
			<!-- the dialog must always exist, Bootstrap looks for it when it is first opened -->
			<div class="modal-dialog modal-lg">
				<div v-if="created" class="modal-content">
					<div class="modal-header">
						<h5 id="ApiKeyModalLabel" class="modal-title">API key for {{ created.Name }}</h5>
						<button type="button" class="btn-close" data-bs-dismiss="modal" aria-label="Close"></button>
					</div>
					<div class="modal-body">
						<div class="alert alert-warning py-2">
							Copy the key now, it is not stored and cannot be shown again. You can generate a new one at
							any time.
						</div>
						<div class="input-group mb-4">
							<input
								class="form-control font-monospace"
								readonly
								:value="created.APIKey"
								data-testid="api-key"
								@focus="$event.target.select()"
							/>
							<button
								class="btn btn-outline-secondary"
								type="button"
								@click="copyToClipboard(created.APIKey)"
							>
								<i
									class="bi"
									:class="copiedText[created.APIKey] ? 'bi-check-lg text-success' : 'bi-clipboard'"
								></i>
								Copy
							</button>
						</div>

						<h6>SMTP</h6>
						<table class="table table-sm mb-3">
							<tbody>
								<tr>
									<th class="w-25">Host</th>
									<td>
										<code>{{ smtpHost }}</code>
									</td>
								</tr>
								<tr>
									<th>Username</th>
									<td>
										<code>{{ created.Slug }}</code>
									</td>
								</tr>
								<tr>
									<th>Password</th>
									<td>the API key</td>
								</tr>
							</tbody>
						</table>

						<h6>API</h6>
						<p class="mb-1">Send the key with every request, using either header:</p>
						<pre class="mb-0"><code>Authorization: Bearer {{ created.APIKey }}
X-API-Key: {{ created.APIKey }}</code></pre>
					</div>
					<div class="modal-footer">
						<button type="button" class="btn btn-primary" data-bs-dismiss="modal">Done</button>
					</div>
				</div>
			</div>
		</div>

		<div
			id="EditProjectModal"
			class="modal fade"
			tabindex="-1"
			aria-labelledby="EditProjectModalLabel"
			aria-hidden="true"
		>
			<div class="modal-dialog">
				<form class="modal-content" @submit.prevent="saveEdit">
					<div class="modal-header">
						<h5 id="EditProjectModalLabel" class="modal-title">Edit project</h5>
						<button type="button" class="btn-close" data-bs-dismiss="modal" aria-label="Close"></button>
					</div>
					<div class="modal-body">
						<div class="mb-3">
							<label for="edit-project-name" class="form-label">Name</label>
							<input
								id="edit-project-name"
								v-model="editing.Name"
								class="form-control"
								required
								maxlength="100"
							/>
						</div>
						<div class="mb-1">
							<label for="edit-project-slug" class="form-label">Slug</label>
							<input
								id="edit-project-slug"
								v-model="editing.Slug"
								class="form-control"
								required
								pattern="[a-z0-9][a-z0-9\-]{0,62}"
								title="Lowercase letters, numbers and hyphens"
							/>
							<div class="form-text">
								The slug is the suggested SMTP username, applications may need updating if you change
								it.
							</div>
						</div>
					</div>
					<div class="modal-footer">
						<button type="button" class="btn btn-outline-secondary" data-bs-dismiss="modal">Cancel</button>
						<button type="submit" class="btn btn-primary">Save</button>
					</div>
				</form>
			</div>
		</div>

		<div
			id="DeleteProjectModal"
			class="modal fade"
			tabindex="-1"
			aria-labelledby="DeleteProjectModalLabel"
			aria-hidden="true"
		>
			<div class="modal-dialog">
				<form class="modal-content" @submit.prevent="remove">
					<div class="modal-header">
						<h5 id="DeleteProjectModalLabel" class="modal-title">Delete {{ deleting.Name }}?</h5>
						<button type="button" class="btn-close" data-bs-dismiss="modal" aria-label="Close"></button>
					</div>
					<div class="modal-body">
						<p>
							This permanently deletes the project and <strong>all of its messages</strong>. This cannot
							be undone.
						</p>
						<label for="delete-project-confirm" class="form-label"
							>Type <code>{{ deleting.Slug }}</code> to confirm</label
						>
						<input
							id="delete-project-confirm"
							v-model="confirmSlug"
							class="form-control"
							autocomplete="off"
						/>
					</div>
					<div class="modal-footer">
						<button type="button" class="btn btn-outline-secondary" data-bs-dismiss="modal">Cancel</button>
						<button type="submit" class="btn btn-danger" :disabled="confirmSlug !== deleting.Slug">
							Delete project
						</button>
					</div>
				</form>
			</div>
		</div>
	</div>
</template>
