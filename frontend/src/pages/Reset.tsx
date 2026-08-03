import "../webmail/webmail.css";
import { useSearchParams } from "react-router-dom";
import { ResetPassword } from "../webmail/PasswordRecovery";

// Route /reset: set a new password from a recovery link.
//
// PUBLIC out of necessity — somebody locked out has no session. The link is what authorises, it
// lasts an hour, works once, and still demands the second factor when the account has one.
export function ResetPage() {
  const [params] = useSearchParams();
  const token = params.get("token") ?? "";
  return (
    <div className="wm">
      <div className="lg-wrap">
        <ResetPassword token={token} />
      </div>
    </div>
  );
}
