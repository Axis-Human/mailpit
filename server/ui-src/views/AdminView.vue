<script>
import AdminProjects from "../components/admin/AdminProjects.vue";
import AdminUsers from "../components/admin/AdminUsers.vue";
import AjaxLoader from "../components/AjaxLoader.vue";
import CommonMixins from "../mixins/CommonMixins";
import { auth, loadAuthStatus } from "../stores/auth";

// Projects & users, only for super admins
export default {
	components: {
		AdminProjects,
		AdminUsers,
		AjaxLoader,
	},

	mixins: [CommonMixins],

	data() {
		return {
			auth,
			tab: "projects",
			projects: [],
			users: [],
		};
	},

	created() {
		if (!auth.user?.IsSuperAdmin) {
			this.$router.replace("/");
			return;
		}

		this.load();
	},

	methods: {
		load() {
			// keeps the project list & status of the inbox in sync with the changes
			loadAuthStatus().catch(this.handleError);

			this.get(this.resolve("/api/v1/admin/projects"), false, (response) => {
				this.projects = response.data;
			});
			this.get(this.resolve("/api/v1/admin/users"), false, (response) => {
				this.users = response.data;
			});
		},
	},
};
</script>

<template>
	<template v-if="auth.user?.IsSuperAdmin">
		<div class="navbar row flex-shrink-0 bg-primary text-white d-print-none" data-bs-theme="dark">
			<div class="col">
				<RouterLink to="/" class="navbar-brand text-white me-0">
					<img :src="resolve('/mailpit.svg')" alt="Mailpit" />
					<span class="ms-2">Mailpit</span>
				</RouterLink>
				<span class="ms-3 text-white-50">Administration</span>
			</div>
			<div class="col-auto">
				<RouterLink to="/" class="btn btn-outline-light btn-sm">
					<i class="bi bi-arrow-left me-1"></i>Back to inbox
				</RouterLink>
			</div>
		</div>

		<div class="row flex-fill overflow-y-auto" style="min-height: 0">
			<div class="col-12 col-xl-10 mx-auto py-3">
				<ul class="nav nav-tabs mb-3">
					<li class="nav-item">
						<button
							class="nav-link"
							:class="{ active: tab === 'projects' }"
							data-testid="tab-projects"
							@click="tab = 'projects'"
						>
							<i class="bi bi-folder-fill me-1"></i>Projects
						</button>
					</li>
					<li class="nav-item">
						<button
							class="nav-link"
							:class="{ active: tab === 'users' }"
							data-testid="tab-users"
							@click="tab = 'users'"
						>
							<i class="bi bi-people-fill me-1"></i>Users
						</button>
					</li>
				</ul>

				<AdminProjects v-if="tab === 'projects'" :projects="projects" @changed="load" />
				<AdminUsers v-else :users="users" :projects="projects" @changed="load" />
			</div>
		</div>

		<AjaxLoader :loading="loading" />
	</template>
</template>
