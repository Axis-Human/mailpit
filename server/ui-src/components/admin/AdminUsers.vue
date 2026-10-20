<script>
import CommonMixins from "../../mixins/CommonMixins";
import { auth } from "../../stores/auth";

const emptyForm = () => ({ ID: "", Email: "", Name: "", Password: "", IsSuperAdmin: false, Projects: [] });

export default {
	mixins: [CommonMixins],

	props: {
		users: {
			type: Array,
			required: true,
		},
		projects: {
			type: Array,
			required: true,
		},
	},

	emits: ["changed"],

	data() {
		return {
			auth,
			form: emptyForm(),
		};
	},

	computed: {
		editingExisting() {
			return this.form.ID !== "";
		},
	},

	methods: {
		projectName(id) {
			return this.projects.find((p) => p.ID === id)?.Name || id;
		},

		add() {
			this.form = emptyForm();
			this.modal("UserModal").show();
		},

		edit(u) {
			this.form = {
				ID: u.ID,
				Email: u.Email,
				Name: u.Name,
				Password: "",
				IsSuperAdmin: u.IsSuperAdmin,
				Projects: [...(u.Projects || [])],
			};
			this.modal("UserModal").show();
		},

		save() {
			const done = () => {
				this.modal("UserModal").hide();
				this.$emit("changed");
			};

			const data = {
				Email: this.form.Email,
				Name: this.form.Name,
				IsSuperAdmin: this.form.IsSuperAdmin,
				Projects: this.form.Projects,
			};

			if (this.editingExisting) {
				// a blank password keeps the current one
				if (this.form.Password) {
					data.Password = this.form.Password;
				}
				this.put(this.resolve(`/api/v1/admin/users/${this.form.ID}`), data, done);
			} else {
				data.Password = this.form.Password;
				this.post(this.resolve("/api/v1/admin/users"), data, done);
			}
		},

		toggle(u) {
			this.put(this.resolve(`/api/v1/admin/users/${u.ID}`), { Disabled: !u.Disabled }, () => {
				this.$emit("changed");
			});
		},

		remove(u) {
			if (!window.confirm(`Delete the user ${u.Email}?`)) {
				return;
			}

			this.delete(this.resolve(`/api/v1/admin/users/${u.ID}`), false, () => {
				this.$emit("changed");
			});
		},
	},
};
</script>

<template>
	<div>
		<div class="mb-3">
			<button class="btn btn-primary" data-testid="add-user" @click="add">
				<i class="bi bi-plus-lg me-1"></i>Add user
			</button>
		</div>

		<div class="table-responsive">
			<table class="table align-middle" data-testid="users-table">
				<thead>
					<tr>
						<th>User</th>
						<th>Role</th>
						<th>Projects</th>
						<th>Status</th>
						<th class="text-end">Actions</th>
					</tr>
				</thead>
				<tbody>
					<tr v-for="u in users" :key="u.ID" :data-testid="'user-' + u.Email">
						<td>
							<div class="fw-bold">{{ u.Email }}</div>
							<div v-if="u.Name" class="small text-muted">{{ u.Name }}</div>
						</td>
						<td>
							<span v-if="u.IsSuperAdmin" class="badge text-bg-primary">Super admin</span>
							<span v-else class="badge text-bg-secondary">User</span>
						</td>
						<td>
							<span v-if="u.IsSuperAdmin" class="text-muted">All projects</span>
							<template v-else>
								<span v-for="id in u.Projects" :key="id" class="badge text-bg-light border me-1">{{
									projectName(id)
								}}</span>
								<span v-if="!u.Projects || !u.Projects.length" class="text-muted">None</span>
							</template>
						</td>
						<td>
							<span v-if="u.Disabled" class="badge text-bg-warning">Disabled</span>
							<span v-else class="badge text-bg-success">Active</span>
						</td>
						<td class="text-end text-nowrap">
							<button class="btn btn-sm btn-outline-secondary me-1" title="Edit" @click="edit(u)">
								<i class="bi bi-pencil-fill"></i>
							</button>
							<button
								class="btn btn-sm me-1"
								:class="u.Disabled ? 'btn-outline-success' : 'btn-outline-warning'"
								:title="u.Disabled ? 'Enable the user' : 'Disable the user'"
								:disabled="u.ID === auth.user.ID"
								@click="toggle(u)"
							>
								<i class="bi" :class="u.Disabled ? 'bi-play-fill' : 'bi-pause-fill'"></i>
							</button>
							<button
								class="btn btn-sm btn-outline-danger"
								title="Delete"
								:disabled="u.ID === auth.user.ID"
								@click="remove(u)"
							>
								<i class="bi bi-trash-fill"></i>
							</button>
						</td>
					</tr>
				</tbody>
			</table>
		</div>

		<div id="UserModal" class="modal fade" tabindex="-1" aria-labelledby="UserModalLabel" aria-hidden="true">
			<div class="modal-dialog">
				<form class="modal-content" data-testid="user-form" @submit.prevent="save">
					<div class="modal-header">
						<h5 id="UserModalLabel" class="modal-title">
							{{ editingExisting ? "Edit user" : "Add user" }}
						</h5>
						<button type="button" class="btn-close" data-bs-dismiss="modal" aria-label="Close"></button>
					</div>
					<div class="modal-body">
						<div class="mb-3">
							<label for="user-email" class="form-label">Email</label>
							<input
								id="user-email"
								v-model="form.Email"
								type="email"
								class="form-control"
								autocomplete="off"
								required
							/>
						</div>
						<div class="mb-3">
							<label for="user-name" class="form-label"
								>Name <span class="text-muted">(optional)</span></label
							>
							<input id="user-name" v-model="form.Name" class="form-control" autocomplete="off" />
						</div>
						<div class="mb-3">
							<label for="user-password" class="form-label">Password</label>
							<input
								id="user-password"
								v-model="form.Password"
								type="password"
								class="form-control"
								autocomplete="new-password"
								minlength="8"
								:required="!editingExisting"
							/>
							<div class="form-text">
								At least 8 characters.
								<template v-if="editingExisting"
									>Leave blank to keep the current password, changing it signs the user out.</template
								>
							</div>
						</div>
						<div class="form-check mb-3">
							<input
								id="user-admin"
								v-model="form.IsSuperAdmin"
								type="checkbox"
								class="form-check-input"
								:disabled="form.ID === auth.user.ID"
							/>
							<label for="user-admin" class="form-check-label">
								Super admin
								<span class="text-muted">- manages projects & users, and can access every project</span>
							</label>
						</div>
						<div v-if="!form.IsSuperAdmin" class="mb-1">
							<div class="form-label">Projects</div>
							<div v-if="!projects.length" class="text-muted small">There are no projects yet.</div>
							<div v-for="p in projects" :key="p.ID" class="form-check">
								<input
									:id="'user-project-' + p.ID"
									v-model="form.Projects"
									type="checkbox"
									class="form-check-input"
									:value="p.ID"
								/>
								<label :for="'user-project-' + p.ID" class="form-check-label">
									{{ p.Name }} <span v-if="p.Disabled" class="badge text-bg-warning">Disabled</span>
								</label>
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
	</div>
</template>
