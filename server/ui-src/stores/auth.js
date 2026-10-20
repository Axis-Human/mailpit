// Authentication & project state for multi-project mode.
// In single mailbox mode (the default) multiProject is false and none of this is used.

import axios from "axios";
import { reactive } from "vue";

const PROJECT_STORAGE_KEY = "mp-project";

export const auth = reactive({
	ready: false, // the status has been loaded
	multiProject: false, // projects & users are enabled, a login is required
	user: null, // the logged in user, {ID, Email, Name, IsSuperAdmin}
	projects: [], // the projects the user has access to
	project: null, // the selected project
});

// Returns the URL for a path relative to the webroot
const apiURL = (path) => {
	const webroot = document.getElementById("app")?.dataset.webroot || "/";
	return webroot.replace(/\/$/, "") + path;
};

// Applies a status response, selecting the remembered project when it is still available
export const applyStatus = (status) => {
	auth.multiProject = !!status.MultiProject;
	auth.user = status.User || null;
	auth.projects = status.Projects || [];
	auth.project = null;

	if (auth.multiProject && auth.user) {
		const saved = localStorage.getItem(PROJECT_STORAGE_KEY);
		auth.project = auth.projects.find((p) => p.Slug === saved) || auth.projects[0] || null;
	}
};

export const loadAuthStatus = async () => {
	try {
		const response = await axios.get(apiURL("/api/v1/auth/status"));
		applyStatus(response.data);
	} finally {
		auth.ready = true;
	}
};

export const login = async (email, password) => {
	const response = await axios.post(apiURL("/api/v1/auth/login"), { Email: email, Password: password });
	applyStatus(response.data);
};

export const logout = async () => {
	try {
		await axios.post(apiURL("/api/v1/auth/logout"));
	} finally {
		// reload to clear everything held in memory
		window.location.assign(apiURL("/"));
	}
};

// Selecting a project reloads the app, which resets the mailbox & the websocket connection
export const selectProject = (slug) => {
	localStorage.setItem(PROJECT_STORAGE_KEY, slug);
	window.location.assign(apiURL("/"));
};

// The project is sent with every request, as the server needs to know which mailbox to use
axios.interceptors.request.use((config) => {
	if (auth.multiProject && auth.project) {
		config.headers["X-Mailpit-Project"] = auth.project.Slug;
	}

	return config;
});

// Return to the login page when the session has expired
axios.interceptors.response.use(
	(response) => response,
	(error) => {
		if (
			auth.multiProject &&
			auth.user &&
			error.response?.status === 401 &&
			!error.config?.url?.includes("/auth/login")
		) {
			auth.user = null;
			auth.project = null;
		}

		return Promise.reject(error);
	},
);
