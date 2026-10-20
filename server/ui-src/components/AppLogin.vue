<script>
import CommonMixins from "../mixins/CommonMixins";
import { login } from "../stores/auth";

export default {
	mixins: [CommonMixins],

	data() {
		return {
			email: "",
			password: "",
			error: "",
			submitting: false,
		};
	},

	methods: {
		submit() {
			if (this.submitting) {
				return;
			}

			this.error = "";
			this.submitting = true;

			login(this.email, this.password)
				.catch((err) => {
					this.error = err.response?.data?.Error || "Unable to log in, please try again.";
					this.password = "";
				})
				.then(() => {
					this.submitting = false;
				});
		},
	},
};
</script>

<template>
	<div class="d-flex align-items-center justify-content-center h-100">
		<form
			class="card shadow-sm"
			style="width: 22rem; max-width: 100%"
			data-testid="login-form"
			@submit.prevent="submit"
		>
			<div class="card-body p-4">
				<h1 class="h4 text-center mb-4">
					<img :src="resolve('/mailpit.svg')" alt="" height="32" class="me-2" />
					Mailpit
				</h1>

				<div v-if="error" class="alert alert-danger py-2" role="alert" data-testid="login-error">
					{{ error }}
				</div>

				<div class="mb-3">
					<label for="login-email" class="form-label">Email</label>
					<input
						id="login-email"
						v-model="email"
						type="email"
						class="form-control"
						autocomplete="username"
						required
						autofocus
					/>
				</div>

				<div class="mb-4">
					<label for="login-password" class="form-label">Password</label>
					<input
						id="login-password"
						v-model="password"
						type="password"
						class="form-control"
						autocomplete="current-password"
						required
					/>
				</div>

				<button type="submit" class="btn btn-primary w-100" :disabled="submitting">Sign in</button>
			</div>
		</form>
	</div>
</template>
