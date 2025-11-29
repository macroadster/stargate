import React from 'react';

const ConfidenceIndicator = ({ confidence }) => {
  if (!confidence || confidence <= 0) {
    return (
      <div className="text-black dark:text-white font-semibold">
        Analysis Required
      </div>
    );
  }

  const confidencePercentage = Math.round(confidence * 100);
  
  return (
    <div className="flex items-center gap-2">
      <div className="text-black dark:text-white font-semibold">
        {confidencePercentage}%
      </div>
      <div className="w-16 bg-gray-200 dark:bg-gray-700 rounded-full h-2">
        <div 
          className="bg-green-500 h-2 rounded-full" 
          style={{width: `${confidencePercentage}%`}}
        ></div>
      </div>
    </div>
  );
};

export default ConfidenceIndicator;