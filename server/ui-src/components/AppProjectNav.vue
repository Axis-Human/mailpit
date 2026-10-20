<script>
import { auth, logout, selectProject } from "../stores/auth";

// Project selector & account menu, shown in the sidebar in multi-project mode
export default {
	data() {
		return {
			auth,
		};
	},

	methods: {
		logout,

		changeProject(e) {
			if (e.target.value !== auth.project?.Slug) {
				selectProject(e.target.value);
			}
		},
	},
};
</script>

<template>
	<div v-if="auth.multiProject && auth.user" class="my-2" data-testid="project-nav">
		<select
			class="form-select form-select-sm mb-2"
			aria-label="Project"
			data-testid="project-select"
			:value="auth.project?.Slug"
			@change="changeProject"
		>
			<option v-for="p in auth.projects" :key="p.ID" :value="p.Slug">
				{{ p.Name }}{{ p.Disabled ? " (disabled)" : "" }}
			</option>
		</select>

		<div v-if="auth.project?.Disabled" class="alert alert-warning py-1 px-2 small mb-2" role="alert">
			This project is disabled: its API key and incoming messages are rejected.
		</div>

		<div class="d-flex align-items-center small text-muted">
			<span class="text-truncate flex-grow-1" :title="auth.user.Email">
				<i class="bi bi-person-fill me-1"></i>{{ auth.user.Name || auth.user.Email }}
			</span>
			<RouterLink
				v-if="auth.user.IsSuperAdmin"
				to="/admin"
				class="btn btn-sm btn-outline-secondary ms-1"
				title="Administration"
				data-testid="admin-link"
			>
				<i class="bi bi-gear-fill"></i>
			</RouterLink>
			<button
				class="btn btn-sm btn-outline-secondary ms-1"
				title="Change password"
				data-bs-toggle="modal"
				data-bs-target="#ChangePasswordModal"
			>
				<i class="bi bi-key-fill"></i>
			</button>
			<button class="btn btn-sm btn-outline-secondary ms-1" title="Sign out" data-testid="logout" @click="logout">
				<i class="bi bi-box-arrow-right"></i>
			</button>
		</div>
	</div>
</template>
