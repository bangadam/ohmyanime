import type { SignInPageProps } from "../types";
import { copy } from "../copy";
import { Button } from "../components";
import "../styles/auth.css";

/**
 * Google is the only accepted identity, so this surface offers one decision and
 * nothing that competes with it.
 */
export default function SignIn({ busy, error, configured, onGoogle, onCancel }: SignInPageProps) {
  return (
    <div className="auth-page">
      <header className="auth-header">
        <span className="wordmark auth-brand">{copy.brand}</span>
      </header>
      <main className="auth-main">
        <section className="auth-card" aria-labelledby="auth-title">
          <h1 id="auth-title" className="auth-title">
            {copy.auth.title}
          </h1>
          <p className="auth-description">{copy.auth.description}</p>

          <div className="auth-actions">
            <Button
              variant="primary"
              className="auth-google"
              icon={<GoogleMark />}
              busy={busy}
              disabled={busy || !configured}
              onClick={onGoogle}
            >
              {busy ? copy.auth.opening : copy.auth.google}
            </Button>
            {busy ? (
              <div className="auth-pending">
                <p className="auth-hint">{copy.auth.browserHint}</p>
                {onCancel ? (
                  <Button variant="quiet" onClick={onCancel}>
                    {copy.auth.cancel}
                  </Button>
                ) : null}
              </div>
            ) : null}
          </div>

          {!configured ? (
            <p className="auth-error" role="alert">
              {copy.auth.notConfigured}
            </p>
          ) : null}
          {error ? (
            <p className="auth-error" role="alert">
              {error}
            </p>
          ) : null}

          <p className="auth-note">{copy.auth.adminNote}</p>
          <p className="visually-hidden" role="status">
            {busy ? copy.auth.waiting : ""}
          </p>
        </section>
      </main>
    </div>
  );
}

/** The Google "G" mark, drawn inline so the page needs no external asset. */
function GoogleMark() {
  return (
    <svg className="auth-google__mark" viewBox="0 0 18 18" width="18" height="18" aria-hidden="true" focusable="false">
      <path
        fill="#4285F4"
        d="M17.64 9.2045c0-.6381-.0573-1.2518-.1636-1.8409H9v3.4814h4.8436c-.2086 1.125-.8427 2.0782-1.7959 2.7164v2.2581h2.9087c1.7018-1.5668 2.6836-3.874 2.6836-6.615z"
      />
      <path
        fill="#34A853"
        d="M9 18c2.43 0 4.4673-.806 5.9564-2.1805l-2.9087-2.2581c-.8059.54-1.8368.859-3.0477.859-2.344 0-4.3282-1.5831-5.036-3.7104H.9574v2.3318C2.4382 15.9832 5.4818 18 9 18z"
      />
      <path fill="#FBBC05" d="M3.964 10.71c-.18-.54-.2822-1.1168-.2822-1.71s.1023-1.17.2823-1.71V4.9582H.9573A8.9965 8.9965 0 0 0 0 9c0 1.4523.3477 2.8268.9573 4.0418L3.964 10.71z" />
      <path
        fill="#EA4335"
        d="M9 3.5795c1.3214 0 2.5077.4541 3.4405 1.346l2.5813-2.5814C13.4632.8918 11.4259 0 9 0 5.4818 0 2.4382 2.0168.9573 4.9582L3.964 7.29C4.6718 5.1627 6.6559 3.5795 9 3.5795z"
      />
    </svg>
  );
}
