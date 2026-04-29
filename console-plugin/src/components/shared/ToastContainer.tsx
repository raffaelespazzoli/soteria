import {
  Alert,
  AlertActionCloseButton,
  AlertActionLink,
  AlertGroup,
} from '@patternfly/react-core';
import { useHistory } from 'react-router-dom';
import { useToastNotifications } from '../../hooks/useToastNotifications';

const MAX_VISIBLE = 4;

const ToastContainer: React.FC = () => {
  const { toasts, removeToast } = useToastNotifications();
  const history = useHistory();

  return (
    <AlertGroup isToast isLiveRegion>
      {toasts.slice(0, MAX_VISIBLE).map((toast) => (
        <Alert
          key={toast.id}
          variant={toast.variant}
          title={toast.title}
          actionClose={
            <AlertActionCloseButton onClose={() => removeToast(toast.id)} />
          }
          actionLinks={
            toast.linkTo ? (
              <AlertActionLink
                onClick={() => {
                  history.push(toast.linkTo!);
                  removeToast(toast.id);
                }}
              >
                {toast.linkText ?? 'View details'}
              </AlertActionLink>
            ) : undefined
          }
        >
          {toast.description}
        </Alert>
      ))}
    </AlertGroup>
  );
};

export default ToastContainer;
