<script>
import CommonMixins from "./mixins/CommonMixins";
import Favicon from "./components/AppFavicon.vue";
import AppBadge from "./components/AppBadge.vue";
import AppLogin from "./components/AppLogin.vue";
import ChangePasswordModal from "./components/ChangePasswordModal.vue";
import Notifications from "./components/AppNotifications.vue";
import EditTags from "./components/EditTags.vue";
import SearchFiltersModal from "./components/SearchFiltersModal.vue";
import { auth, loadAuthStatus, logout } from "./stores/auth";
import { mailbox } from "./stores/mailbox";

export default {
	components: {
		Favicon,
		AppBadge,
		AppLogin,
		ChangePasswordModal,
		Notifications,
		EditTags,
		SearchFiltersModal,
	},

	mixins: [CommonMixins],

	data() {
		return {
			auth,
		};
	},

	computed: {
		// multi-project mode requires a login
		showLogin() {
			return auth.ready && auth.multiProject && !auth.user;
		},

		// super admins can still reach the administration to create the first project
		adminWithoutProject() {
			return !!auth.user?.IsSuperAdmin && this.$route.path === "/admin";
		},

		// a user without any projects has nothing to show
		showNoProject() {
			return auth.ready && auth.multiProject && !!auth.user && !auth.project && !this.adminWithoutProject;
		},

		showApp() {
			return auth.ready && (!auth.multiProject || (!!auth.user && (!!auth.project || this.adminWithoutProject)));
		},
	},

	watch: {
		$route() {
			// hide mobile menu on URL change
			this.hideNav();
		},

		showApp(show) {
			// the global config is only available once logged in
			if (show) {
				this.loadConfig();
			}
		},
	},

	beforeMount() {
		loadAuthStatus().catch(this.handleError);
	},

	methods: {
		logout,

		loadConfig() {
			this.get(this.resolve("/api/v1/webui"), false, (response) => {
				mailbox.uiConfig = response.data;

				if (mailbox.uiConfig.Label) {
					document.title = document.title + " - " + mailbox.uiConfig.Label;
				} else {
					document.title = document.title + " - " + location.hostname;
				}
			});
		},
	},
};
</script>

<template>
	<AppLogin v-if="showLogin" />

	<div v-else-if="showNoProject" class="d-flex align-items-center justify-content-center h-100">
		<div class="text-center" data-testid="no-project">
			<img :src="resolve('/mailpit.svg')" alt="Mailpit" style="width: 80px" />
			<h1 class="h4 mt-3">No projects</h1>
			<p v-if="auth.user.IsSuperAdmin">Create the first project to start receiving messages.</p>
			<p v-else>You have not been given access to any project yet, please ask an administrator.</p>
			<RouterLink v-if="auth.user.IsSuperAdmin" to="/admin" class="btn btn-primary me-2"
				>Administration</RouterLink
			>
			<button class="btn btn-outline-secondary" @click="logout">Sign out</button>
		</div>
	</div>

	<template v-else-if="showApp">
		<RouterView />
		<Favicon />
		<AppBadge />
		<!-- the websocket is for a project, so wait for one -->
		<Notifications v-if="!auth.multiProject || auth.project" />
		<EditTags />
		<SearchFiltersModal />
		<ChangePasswordModal v-if="auth.multiProject" />
	</template>
</template>
