<script>
import CommonMixins from "../mixins/CommonMixins";

export default {
	mixins: [CommonMixins],

	data() {
		return {
			current: "",
			password: "",
			confirm: "",
			error: "",
		};
	},

	computed: {
		mismatch() {
			return this.confirm !== "" && this.password !== this.confirm;
		},
	},

	mounted() {
		// clear the form whenever the dialog closes
		document.getElementById("ChangePasswordModal")?.addEventListener("hidden.bs.modal", this.reset);
	},

	methods: {
		reset() {
			this.current = "";
			this.password = "";
			this.confirm = "";
			this.error = "";
		},

		save() {
			this.error = "";

			if (this.password !== this.confirm) {
				this.error = "The passwords do not match.";
				return;
			}

			this.put(this.resolve("/api/v1/auth/password"), { Current: this.current, New: this.password }, () => {
				this.modal("ChangePasswordModal").hide();
			});
		},
	},
};
</script>

<template>
	<div
		id="ChangePasswordModal"
		class="modal fade"
		tabindex="-1"
		aria-labelledby="ChangePasswordModalLabel"
		aria-hidden="true"
	>
		<div class="modal-dialog">
			<form class="modal-content" @submit.prevent="save">
				<div class="modal-header">
					<h5 id="ChangePasswordModalLabel" class="modal-title">Change password</h5>
					<button type="button" class="btn-close" data-bs-dismiss="modal" aria-label="Close"></button>
				</div>
				<div class="modal-body">
					<div v-if="error" class="alert alert-danger py-2">{{ error }}</div>
					<div class="mb-3">
						<label for="cp-current" class="form-label">Current password</label>
						<input
							id="cp-current"
							v-model="current"
							type="password"
							class="form-control"
							autocomplete="current-password"
							required
						/>
					</div>
					<div class="mb-3">
						<label for="cp-new" class="form-label">New password</label>
						<input
							id="cp-new"
							v-model="password"
							type="password"
							class="form-control"
							autocomplete="new-password"
							minlength="8"
							required
						/>
						<div class="form-text">
							At least 8 characters. You will be signed out of all other browsers.
						</div>
					</div>
					<div class="mb-1">
						<label for="cp-confirm" class="form-label">Confirm new password</label>
						<input
							id="cp-confirm"
							v-model="confirm"
							type="password"
							class="form-control"
							:class="{ 'is-invalid': mismatch }"
							autocomplete="new-password"
							required
						/>
					</div>
				</div>
				<div class="modal-footer">
					<button type="button" class="btn btn-outline-secondary" data-bs-dismiss="modal">Cancel</button>
					<button type="submit" class="btn btn-primary">Change password</button>
				</div>
			</form>
		</div>
	</div>
</template>
