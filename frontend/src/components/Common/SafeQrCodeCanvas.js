import React from 'react';
import { QRCodeCanvas } from 'qrcode.react';

class QrCodeErrorBoundary extends React.Component {
  constructor(props) {
    super(props);
    this.state = { hasError: false };
  }

  static getDerivedStateFromError() {
    return { hasError: true };
  }

  render() {
    const { hasError } = this.state;
    const { fallback, children } = this.props;

    if (hasError) {
      return fallback || <div className="text-xs text-amber-600">QR data too large to render.</div>;
    }

    return children;
  }
}

const SafeQrCodeCanvas = ({ value, fallback, ...props }) => (
  <QrCodeErrorBoundary fallback={fallback}>
    <QRCodeCanvas value={value} {...props} />
  </QrCodeErrorBoundary>
);

export default SafeQrCodeCanvas;
